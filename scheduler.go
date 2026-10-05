package main

import (
	"sort"

	"github.com/anacrolix/torrent"
)

func remainingTorrentBytes(t *torrent.Torrent) int64 {
	if t.Info() == nil {
		return -1
	}
	remaining := t.Length() - t.BytesCompleted()
	if remaining < 0 {
		return 0
	}
	return remaining
}

func addReservedSpace(reserved uint64, remaining int64) uint64 {
	if remaining <= 0 {
		return reserved
	}
	bytes := uint64(remaining)
	if bytes > ^uint64(0)-reserved {
		return ^uint64(0)
	}
	return reserved + bytes
}

func (a *app) scheduleDownloads() {
	if a.client == nil {
		return
	}
	a.schedulerMu.Lock()
	defer a.schedulerMu.Unlock()

	torrents := a.client.Torrents()
	records := make(map[string]torrentRecord, len(torrents))
	a.mu.RLock()
	for hash, record := range a.records {
		records[hash] = record
	}
	a.mu.RUnlock()
	sort.SliceStable(torrents, func(i, j int) bool {
		leftRecord := records[torrents[i].InfoHash().HexString()]
		rightRecord := records[torrents[j].InfoHash().HexString()]
		if leftRecord.StartImmediately != rightRecord.StartImmediately {
			return leftRecord.StartImmediately
		}
		left := leftRecord.AddedAt
		right := rightRecord.AddedAt
		if left == right {
			return torrents[i].InfoHash().HexString() < torrents[j].InfoHash().HexString()
		}
		return left < right
	})

	var pending, pauseForLimit []*torrent.Torrent
	var activeCount int
	var reservedSpace uint64
	a.mu.Lock()
	limit := a.config.MaxDownloads
	if !validDownloadLimit(limit) {
		limit = 3
	}
	for _, t := range torrents {
		hash := t.InfoHash().HexString()
		record := a.records[hash]
		if a.paused[hash] || record.Paused {
			delete(a.activeDownloads, hash)
			a.queueReasons[hash] = "Paused"
			continue
		}
		remaining := remainingTorrentBytes(t)
		if remaining < 0 {
			delete(a.activeDownloads, hash)
			a.queueReasons[hash] = "Fetching metadata"
			continue
		}
		if remaining == 0 {
			delete(a.activeDownloads, hash)
			delete(a.queueReasons, hash)
			continue
		}
		if a.activeDownloads[hash] && (limit == 0 || activeCount < limit) {
			activeCount++
			reservedSpace = addReservedSpace(reservedSpace, remaining)
			delete(a.queueReasons, hash)
			continue
		}
		if a.activeDownloads[hash] {
			delete(a.activeDownloads, hash)
			a.queueReasons[hash] = "Waiting for an active slot"
			pauseForLimit = append(pauseForLimit, t)
			continue
		}
		pending = append(pending, t)
	}
	a.mu.Unlock()

	for _, t := range pauseForLimit {
		for _, file := range t.Files() {
			file.SetPriority(torrent.PiecePriorityNone)
		}
	}

	recordsChanged := false
	for _, t := range pending {
		hash := t.InfoHash().HexString()
		remaining := remainingTorrentBytes(t)
		freeSpace := storageInfo(a.root)["free"]

		a.mu.Lock()
		record := a.records[hash]
		if a.paused[hash] || record.Paused {
			a.queueReasons[hash] = "Paused"
			a.mu.Unlock()
			continue
		}
		if a.activeDownloads[hash] {
			a.mu.Unlock()
			continue
		}
		limit = a.config.MaxDownloads
		if !validDownloadLimit(limit) {
			limit = 3
		}
		if limit > 0 && activeCount >= limit {
			a.queueReasons[hash] = "Waiting for an active slot"
			a.mu.Unlock()
			continue
		}
		if remaining < 0 {
			a.queueReasons[hash] = "Fetching metadata"
			a.mu.Unlock()
			continue
		}
		if !hasStorageCapacity(freeSpace, reservedSpace, remaining) {
			a.queueReasons[hash] = "Waiting for disk space"
			a.mu.Unlock()
			continue
		}
		a.activeDownloads[hash] = true
		if record.StartImmediately {
			record.StartImmediately = false
			a.records[hash] = record
			recordsChanged = true
		}
		delete(a.queueReasons, hash)
		t.DownloadAll()
		activeCount++
		reservedSpace = addReservedSpace(reservedSpace, remaining)
		a.mu.Unlock()
	}
	if recordsChanged {
		a.saveTorrentRecords()
	}
}

func (a *app) queuePosition(hash string) int {
	type queuedTorrent struct {
		addedAt int64
		hash    string
		direct  bool
	}
	items := make([]queuedTorrent, 0)
	a.mu.RLock()
	paused := make(map[string]bool, len(a.paused))
	active := make(map[string]bool, len(a.activeDownloads))
	records := make(map[string]torrentRecord, len(a.records))
	for key, value := range a.paused {
		paused[key] = value
	}
	for key, value := range a.activeDownloads {
		active[key] = value
	}
	for key, value := range a.records {
		records[key] = value
	}
	a.mu.RUnlock()
	for _, t := range a.client.Torrents() {
		key := t.InfoHash().HexString()
		if paused[key] || records[key].Paused || active[key] || remainingTorrentBytes(t) == 0 {
			continue
		}
		items = append(items, queuedTorrent{addedAt: records[key].AddedAt, hash: key, direct: records[key].StartImmediately})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].direct != items[j].direct {
			return items[i].direct
		}
		if items[i].addedAt == items[j].addedAt {
			return items[i].hash < items[j].hash
		}
		return items[i].addedAt < items[j].addedAt
	})
	for index, item := range items {
		if item.hash == hash {
			return index + 1
		}
	}
	return 0
}

func (a *app) moveQueuedTorrent(hash string, direction int) bool {
	a.schedulerMu.Lock()
	type queuedTorrent struct {
		hash    string
		addedAt int64
		direct  bool
	}
	items := make([]queuedTorrent, 0)
	a.mu.RLock()
	for _, t := range a.client.Torrents() {
		key := t.InfoHash().HexString()
		record := a.records[key]
		if a.paused[key] || record.Paused || a.activeDownloads[key] || remainingTorrentBytes(t) == 0 {
			continue
		}
		items = append(items, queuedTorrent{hash: key, addedAt: record.AddedAt, direct: record.StartImmediately})
	}
	a.mu.RUnlock()
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].direct != items[j].direct {
			return items[i].direct
		}
		if items[i].addedAt == items[j].addedAt {
			return items[i].hash < items[j].hash
		}
		return items[i].addedAt < items[j].addedAt
	})
	index := -1
	for i, item := range items {
		if item.hash == hash {
			index = i
			break
		}
	}
	target := index + direction
	if index < 0 || target < 0 || target >= len(items) {
		a.schedulerMu.Unlock()
		return false
	}
	items[index].direct, items[target].direct = items[target].direct, items[index].direct
	items[index], items[target] = items[target], items[index]
	base := items[0].addedAt
	a.mu.Lock()
	for i, item := range items {
		record := a.records[item.hash]
		record.AddedAt = base + int64(i)
		record.StartImmediately = item.direct
		a.records[item.hash] = record
	}
	a.mu.Unlock()
	a.schedulerMu.Unlock()
	a.saveTorrentRecords()
	a.scheduleDownloads()
	return true
}
