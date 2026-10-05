package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type app struct {
	client          *torrent.Client
	auth            *authStore
	root            string
	config          settings
	mu              sync.RWMutex
	schedulerMu     sync.Mutex
	processUsageMu  sync.Mutex
	processSample   processUsageSample
	paused          map[string]bool
	activeDownloads map[string]bool
	queueReasons    map[string]string
	samples         map[string]speedSample
	records         map[string]torrentRecord
}

type settings struct {
	DownloadPath string `json:"downloadPath"`
	Seeding      bool   `json:"seeding"`
	Upload       bool   `json:"upload"`
	MaxDownloads int    `json:"maxDownloads"`
}

type speedSample struct {
	at         time.Time
	downloaded int64
	uploaded   int64
}

type processUsageSample struct {
	at       time.Time
	cpu      time.Duration
	receive  uint64
	transmit uint64
}

type torrentRecord struct {
	Hash             string `json:"hash"`
	Magnet           string `json:"magnet,omitempty"`
	MetaFile         string `json:"metaFile,omitempty"`
	Paused           bool   `json:"paused,omitempty"`
	AddedAt          int64  `json:"addedAt,omitempty"`
	StartImmediately bool   `json:"startImmediately,omitempty"`
}

type processUsageView struct {
	CPUPercent       float64 `json:"cpuPercent"`
	MemoryBytes      uint64  `json:"memoryBytes"`
	ReceiveRate      uint64  `json:"receiveRate"`
	TransmitRate     uint64  `json:"transmitRate"`
	NetworkAvailable bool    `json:"networkAvailable"`
	UpdatedAt        string  `json:"updatedAt"`
}

type torrentView struct {
	Hash          string     `json:"hash"`
	Name          string     `json:"name"`
	Size          int64      `json:"size"`
	Downloaded    int64      `json:"downloaded"`
	Progress      float64    `json:"progress"`
	Rate          int64      `json:"rate"`
	Peers         int        `json:"peers"`
	Status        string     `json:"status"`
	AddedAt       string     `json:"addedAt"`
	Files         []fileView `json:"files"`
	DownloadSpeed int64      `json:"downloadSpeed"`
	UploadSpeed   int64      `json:"uploadSpeed"`
	QueuePosition int        `json:"queuePosition,omitempty"`
	QueueReason   string     `json:"queueReason,omitempty"`
	DownloadLimit int        `json:"downloadLimit"`
}

type fileView struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
	Complete   bool   `json:"complete"`
}

const minimumFreeSpaceReserve uint64 = 64 << 20

func hasStorageCapacity(freeSpace, reservedSpace uint64, torrentSize int64) bool {
	if torrentSize < 0 {
		return false
	}
	required := uint64(torrentSize)
	if required > freeSpace || reservedSpace > freeSpace-required {
		return false
	}
	return freeSpace-required-reservedSpace >= minimumFreeSpaceReserve
}

func validDownloadLimit(limit int) bool {
	return limit == 0 || (limit >= 2 && limit <= 5)
}

type searchResult struct {
	Title  string `json:"title"`
	Size   string `json:"size"`
	Seeds  int    `json:"seeds"`
	Peers  int    `json:"peers"`
	Date   string `json:"date"`
	Magnet string `json:"magnet"`
	Link   string `json:"link"`
	Source string `json:"source"`
}

func main() {
	if len(os.Args) > 1 || path.Base(os.Args[0]) == "ctd" {
		if err := runAdminCommand(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	config := loadSettings()
	root := os.Getenv("DOWNLOAD_DIR")
	if root == "" {
		root = config.DownloadPath
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatal(err)
	}
	stateDir := stateDirectory()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		log.Fatal(err)
	}
	auth, err := openAuthStore(filepath.Join(stateDir, "accounts.sqlite"))
	if err != nil {
		log.Fatal(err)
	}
	defer auth.close()
	engineDir := filepath.Join(stateDir, ".cloud-torrent-data")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		log.Fatal(err)
	}
	moveLegacyEngineFiles(root, engineDir)
	cfg := torrent.NewDefaultClientConfig()
	if listenPort := os.Getenv("TORRENT_LISTEN_PORT"); listenPort != "" {
		port, err := strconv.Atoi(listenPort)
		if err != nil || port < 0 || port > 65535 {
			log.Fatal("TORRENT_LISTEN_PORT must be between 0 and 65535")
		}
		cfg.ListenPort = port
	}
	pieceCompletion, err := storage.NewDefaultPieceCompletionForDir(engineDir)
	if err != nil {
		log.Fatal(err)
	}
	fileStorage := storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: root, PieceCompletion: pieceCompletion})
	defer fileStorage.Close()
	cfg.DataDir = engineDir
	cfg.DefaultStorage = fileStorage
	cfg.NoUpload = !config.Upload
	cfg.Seed = config.Seeding
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	a := &app{
		client: client, auth: auth, root: root, config: config,
		paused: make(map[string]bool), activeDownloads: make(map[string]bool),
		queueReasons: make(map[string]string), samples: make(map[string]speedSample),
		records: loadTorrentRecords(),
	}
	a.restoreTorrents()
	a.scheduleDownloads()
	go a.runScheduler()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/torrents", a.handleTorrents)
	mux.HandleFunc("/api/torrent", a.handleTorrent)
	mux.HandleFunc("/api/torrent/", a.handleTorrentRoute)
	mux.HandleFunc("/api/torrent-file", a.handleTorrentFile)
	mux.HandleFunc("/download/", a.handleDownload)
	mux.HandleFunc("/download-zip/", a.handleDownloadZip)
	mux.HandleFunc("/stream/", a.handleStream)
	mux.HandleFunc("/api/search", a.handleSearch)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, map[string]bool{"ok": true}) })
	mux.HandleFunc("/api/settings", a.handleSettings)
	mux.HandleFunc("/api/storage", a.handleStorage)
	mux.HandleFunc("/api/process", a.handleProcessUsage)
	mux.HandleFunc("/api/auth/status", a.handleAuthStatus)
	mux.HandleFunc("/api/auth/register", a.handleAuthRegister)
	mux.HandleFunc("/api/auth/login", a.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", a.handleAuthLogout)
	mux.HandleFunc("/api/profile", a.handleProfile)
	mux.HandleFunc("/api/auth/password", a.handlePasswordChange)
	registerWebPages(mux)
	listenAddr := os.Getenv("HTTP_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	server := &http.Server{Addr: listenAddr, Handler: logging(a.requireLogin(mux)), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("Magnetor listening on http://localhost%s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

// registerWebPages wires up the HTML pages (kept under web/html for a tidy,
// folder-wise layout) while leaving their public URLs unchanged, and falls
// back to a plain file server for static assets (web/css, web/js, etc).
func registerWebPages(mux *http.ServeMux) {
	servePage := func(name string) http.HandlerFunc {
		file := filepath.Join("web", "html", name)
		return func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, file) }
	}
	mux.HandleFunc("/login.html", servePage("login.html"))
	mux.HandleFunc("/profile.html", servePage("profile.html"))
	mux.HandleFunc("/settings.html", servePage("settings.html"))
	index := servePage("index.html")
	assets := http.FileServer(http.Dir("./web"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			index(w, r)
			return
		}
		assets.ServeHTTP(w, r)
	})
}

func moveLegacyEngineFiles(downloadPath, enginePath string) {
	for _, name := range []string{".torrent.db", ".torrent.db-shm", ".torrent.db-wal"} {
		source := filepath.Join(downloadPath, name)
		target := filepath.Join(enginePath, name)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.Rename(source, target); err != nil {
			log.Printf("could not move engine file %s: %v", source, err)
		}
	}
}

func loadSettings() settings {
	config := settings{DownloadPath: "./data/downloads", MaxDownloads: 3}
	data, err := os.ReadFile(filepath.Join(stateDirectory(), "cloud-torrent.json"))
	if err == nil {
		_ = json.Unmarshal(data, &config)
	}
	if !validDownloadLimit(config.MaxDownloads) {
		config.MaxDownloads = 3
	}
	return config
}

func stateDirectory() string {
	if value := os.Getenv("STATE_DIR"); value != "" {
		return value
	}
	return "./data/state"
}

func loadTorrentRecords() map[string]torrentRecord {
	records := make(map[string]torrentRecord)
	data, err := os.ReadFile(filepath.Join(stateDirectory(), "torrents.json"))
	if err == nil {
		_ = json.Unmarshal(data, &records)
	}
	return records
}

func (a *app) saveTorrentRecords() {
	a.mu.RLock()
	data, err := json.MarshalIndent(a.records, "", "  ")
	a.mu.RUnlock()
	if err == nil {
		_ = os.WriteFile(filepath.Join(stateDirectory(), "torrents.json"), data, 0o644)
	}
}

func (a *app) rememberTorrent(record torrentRecord) {
	a.mu.Lock()
	a.records[record.Hash] = record
	a.mu.Unlock()
	a.saveTorrentRecords()
}

func (a *app) restoreTorrents() {
	a.mu.RLock()
	records := make([]torrentRecord, 0, len(a.records))
	for _, record := range a.records {
		records = append(records, record)
	}
	a.mu.RUnlock()
	for _, record := range records {
		var t *torrent.Torrent
		var err error
		if record.MetaFile != "" {
			t, err = a.client.AddTorrentFromFile(record.MetaFile)
		} else if record.Magnet != "" {
			t, err = a.client.AddMagnet(record.Magnet)
		}
		if err != nil || t == nil {
			log.Printf("could not restore torrent %s: %v", record.Hash, err)
			continue
		}
		if record.Paused {
			a.mu.Lock()
			a.paused[record.Hash] = true
			a.mu.Unlock()
			if t.Info() != nil {
				for _, file := range t.Files() {
					file.SetPriority(torrent.PiecePriorityNone)
				}
			}
		}
	}
}

func (a *app) runScheduler() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.scheduleDownloads()
	}
}

func (a *app) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		config := loadSettings()
		writeJSON(w, map[string]any{"settings": config, "storage": storageInfo(config.DownloadPath)})
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		DownloadPath string `json:"downloadPath"`
		Seeding      bool   `json:"seeding"`
		Upload       bool   `json:"upload"`
		MaxDownloads *int   `json:"maxDownloads"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.DownloadPath) == "" {
		writeError(w, http.StatusBadRequest, "download path is required")
		return
	}
	a.mu.RLock()
	previous := a.config
	a.mu.RUnlock()
	maxDownloads := previous.MaxDownloads
	if body.MaxDownloads != nil {
		maxDownloads = *body.MaxDownloads
	}
	if !validDownloadLimit(maxDownloads) {
		writeError(w, http.StatusBadRequest, "parallel downloads must be 2 to 5 or unlimited")
		return
	}
	next := settings{DownloadPath: body.DownloadPath, Seeding: body.Seeding, Upload: body.Upload, MaxDownloads: maxDownloads}
	path, err := filepath.Abs(next.DownloadPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid download path")
		return
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		writeError(w, http.StatusBadRequest, "download path is not writable")
		return
	}
	next.DownloadPath = path
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil || os.WriteFile(filepath.Join(stateDirectory(), "cloud-torrent.json"), data, 0o644) != nil {
		writeError(w, http.StatusInternalServerError, "could not save settings")
		return
	}
	a.mu.Lock()
	a.config = next
	a.mu.Unlock()
	a.scheduleDownloads()
	activePath, pathErr := filepath.Abs(a.root)
	if pathErr != nil {
		activePath = filepath.Clean(a.root)
	}
	restartRequired := next.DownloadPath != activePath || next.Upload != previous.Upload || next.Seeding != previous.Seeding
	writeJSON(w, map[string]any{"settings": next, "restartRequired": restartRequired})
}

func (a *app) handleStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, storageInfo(a.root))
}

func storageInfo(path string) map[string]uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return map[string]uint64{"total": 0, "used": 0, "free": 0}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - (stat.Bfree * uint64(stat.Bsize))
	return map[string]uint64{"total": total, "used": used, "free": free}
}

func (a *app) handleProcessUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	now := time.Now()
	cpu, cpuErr := readProcessCPUTime()
	memory, _ := readProcessMemory()
	receive, transmit, networkErr := readNetworkTotals()
	usage := processUsageView{MemoryBytes: memory, NetworkAvailable: networkErr == nil, UpdatedAt: now.UTC().Format(time.RFC3339)}
	a.processUsageMu.Lock()
	previous := a.processSample
	if cpuErr == nil && !previous.at.IsZero() {
		elapsed := now.Sub(previous.at)
		cpuDelta := cpu - previous.cpu
		if elapsed > 0 && cpuDelta >= 0 {
			usage.CPUPercent = float64(cpuDelta) / float64(elapsed) * 100
		}
	}
	if networkErr == nil && !previous.at.IsZero() {
		elapsed := now.Sub(previous.at).Seconds()
		if elapsed > 0 && receive >= previous.receive && transmit >= previous.transmit {
			usage.ReceiveRate = uint64(float64(receive-previous.receive) / elapsed)
			usage.TransmitRate = uint64(float64(transmit-previous.transmit) / elapsed)
		}
	}
	if cpuErr == nil {
		a.processSample.at = now
		a.processSample.cpu = cpu
	}
	if networkErr == nil {
		a.processSample.at = now
		a.processSample.receive = receive
		a.processSample.transmit = transmit
	}
	a.processUsageMu.Unlock()
	writeJSON(w, usage)
}

func readProcessCPUTime() (time.Duration, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, err
	}
	user := time.Duration(usage.Utime.Sec)*time.Second + time.Duration(usage.Utime.Usec)*time.Microsecond
	system := time.Duration(usage.Stime.Sec)*time.Second + time.Duration(usage.Stime.Usec)*time.Microsecond
	return user + system, nil
}

func readProcessMemory() (uint64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return kilobytes * 1024, nil
	}
	return 0, errors.New("process memory is unavailable")
}

func readNetworkTotals() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	var received, transmitted uint64
	for _, line := range strings.Split(string(data), "\n") {
		_, values, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 9 {
			continue
		}
		rx, rxErr := strconv.ParseUint(fields[0], 10, 64)
		tx, txErr := strconv.ParseUint(fields[8], 10, 64)
		if rxErr != nil || txErr != nil {
			continue
		}
		received += rx
		transmitted += tx
	}
	return received, transmitted, nil
}

func (a *app) handleTorrents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	items := make([]torrentView, 0)
	for _, t := range a.client.Torrents() {
		items = append(items, a.view(t))
	}
	writeJSON(w, items)
}

func (a *app) handleTorrent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Magnet string `json:"magnet"`
			URL    string `json:"url"`
			Mode   string `json:"mode"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		mode := strings.ToLower(strings.TrimSpace(body.Mode))
		if mode == "" {
			mode = "queue"
		}
		if mode != "queue" && mode != "direct" {
			writeError(w, http.StatusBadRequest, "mode must be queue or direct")
			return
		}
		var t *torrent.Torrent
		var err error
		if strings.TrimSpace(body.Magnet) != "" {
			t, err = a.client.AddMagnet(strings.TrimSpace(body.Magnet))
		} else if body.URL != "" {
			err = errors.New("remote torrent URLs are disabled; use a magnet link")
		} else {
			err = errors.New("magnet is required")
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.rememberTorrent(torrentRecord{Hash: t.InfoHash().HexString(), Magnet: strings.TrimSpace(body.Magnet), AddedAt: time.Now().UnixNano(), StartImmediately: mode == "direct"})
		a.startWhenReady(t)
		writeJSON(w, a.view(t))
	case http.MethodDelete:
		hash := strings.TrimPrefix(r.URL.Path, "/api/torrent/")
		t, ok := a.find(hash)
		if !ok {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		a.removeTorrent(t)
		writeJSON(w, map[string]bool{"ok": true})
	case http.MethodPatch:
		hash := strings.TrimPrefix(r.URL.Path, "/api/torrent/")
		t, ok := a.find(hash)
		if !ok {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if body.Action == "start" {
			a.startWhenReady(t)
		} else if body.Action == "stop" {
			a.pause(t)
		} else if body.Action == "queue-up" {
			if !a.moveQueuedTorrent(hash, -1) {
				writeError(w, http.StatusConflict, "torrent cannot move higher in the queue")
				return
			}
		} else if body.Action == "queue-down" {
			if !a.moveQueuedTorrent(hash, 1) {
				writeError(w, http.StatusConflict, "torrent cannot move lower in the queue")
				return
			}
		} else {
			writeError(w, http.StatusBadRequest, "action must be start, stop, queue-up, or queue-down")
			return
		}
		writeJSON(w, a.view(t))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *app) handleTorrentRoute(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/files") {
		hash := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/torrent/"), "/files")
		t, ok := a.find(hash)
		if !ok {
			writeError(w, http.StatusNotFound, "torrent not found")
			return
		}
		writeJSON(w, a.files(t))
		return
	}
	a.handleTorrent(w, r)
}

func (a *app) handleTorrentFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "torrent file is too large or invalid")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.FormValue("mode")))
	if mode == "" {
		mode = "queue"
	}
	if mode != "queue" && mode != "direct" {
		writeError(w, http.StatusBadRequest, "mode must be queue or direct")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "torrent file is required")
		return
	}
	defer file.Close()
	meta, err := metainfo.Load(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid torrent file")
		return
	}
	t, err := a.client.AddTorrent(meta)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash := t.InfoHash().HexString()
	metaDir := filepath.Join(stateDirectory(), ".torrent-metadata")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save torrent metadata")
		return
	}
	metaFile := filepath.Join(metaDir, hash+".torrent")
	metadata, err := os.Create(metaFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save torrent metadata")
		return
	}
	writeErr := meta.Write(metadata)
	_ = metadata.Close()
	if writeErr != nil {
		writeError(w, http.StatusInternalServerError, "could not save torrent metadata")
		return
	}
	a.rememberTorrent(torrentRecord{Hash: hash, MetaFile: metaFile, AddedAt: time.Now().UnixNano(), StartImmediately: mode == "direct"})
	a.startWhenReady(t)
	writeJSON(w, a.view(t))
}

func (a *app) handleDownload(w http.ResponseWriter, r *http.Request) {
	a.serveTorrentFile(w, r, "/download/", "attachment")
}

func (a *app) handleStream(w http.ResponseWriter, r *http.Request) {
	a.serveTorrentFile(w, r, "/stream/", "inline")
}

func (a *app) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, "/download-zip/")
	t, ok := a.find(hash)
	if !ok || t.Info() == nil {
		writeError(w, http.StatusNotFound, "torrent or metadata not found")
		return
	}
	files := t.Files()
	if len(files) < 2 {
		writeError(w, http.StatusBadRequest, "ZIP download requires a torrent with multiple files")
		return
	}

	type archiveFile struct {
		name string
		path string
		info os.FileInfo
	}
	root, err := filepath.Abs(a.root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid download directory")
		return
	}
	archiveFiles := make([]archiveFile, 0, len(files))
	for _, file := range files {
		if file.BytesCompleted() < file.Length() {
			writeError(w, http.StatusConflict, "all torrent files must be complete before creating a ZIP")
			return
		}
		name := path.Clean(strings.ReplaceAll(file.Path(), "\\", "/"))
		if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			writeError(w, http.StatusForbidden, "torrent contains an invalid file path")
			return
		}
		filePath, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file path")
			return
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			writeError(w, http.StatusForbidden, "file path is outside download directory")
			return
		}
		info, err := os.Stat(filePath)
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusConflict, "a completed torrent file is not available on disk")
			return
		}
		archiveFiles = append(archiveFiles, archiveFile{name: name, path: filePath, info: info})
	}

	archiveName := path.Base(strings.ReplaceAll(t.Name(), "\\", "/"))
	if archiveName == "" || archiveName == "." || archiveName == "/" {
		archiveName = "torrent-files"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": archiveName + ".zip"}))
	zipWriter := zip.NewWriter(w)
	for _, file := range archiveFiles {
		header, err := zip.FileInfoHeader(file.info)
		if err != nil {
			log.Printf("could not create ZIP header for %s: %v", file.name, err)
			break
		}
		header.Name = file.name
		header.Method = zip.Deflate
		entry, err := zipWriter.CreateHeader(header)
		if err != nil {
			log.Printf("could not create ZIP entry for %s: %v", file.name, err)
			break
		}
		input, err := os.Open(file.path)
		if err != nil {
			log.Printf("could not open file for ZIP %s: %v", file.path, err)
			break
		}
		_, copyErr := io.Copy(entry, input)
		closeErr := input.Close()
		if copyErr != nil || closeErr != nil {
			log.Printf("could not write ZIP entry %s: %v", file.name, errors.Join(copyErr, closeErr))
			break
		}
	}
	if err := zipWriter.Close(); err != nil {
		log.Printf("could not finish torrent ZIP: %v", err)
	}
}

func (a *app) serveTorrentFile(w http.ResponseWriter, r *http.Request, route, disposition string) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, route), "/", 2)
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "file path is required")
		return
	}
	t, ok := a.find(parts[0])
	if !ok || t.Info() == nil {
		writeError(w, http.StatusNotFound, "torrent or metadata not found")
		return
	}
	filePath, err := url.PathUnescape(parts[1])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}
	file := a.fileByPath(t, filePath)
	if file == nil || file.BytesCompleted() < file.Length() {
		writeError(w, http.StatusConflict, "file is not complete yet")
		return
	}
	root, err := filepath.Abs(a.root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid download directory")
		return
	}
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(file.Path())))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file path")
		return
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		writeError(w, http.StatusForbidden, "file path is outside download directory")
		return
	}
	filename := filepath.Base(path)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	if disposition == "inline" {
		contentType := mime.TypeByExtension(filepath.Ext(filename))
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".mkv":
			contentType = "video/x-matroska"
		case ".flv":
			contentType = "video/x-flv"
		case ".f4v":
			contentType = "video/mp4"
		case ".m4v":
			contentType = "video/mp4"
		case ".wmv":
			contentType = "video/x-ms-wmv"
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}
	http.ServeFile(w, r, path)
}

func (a *app) startWhenReady(t *torrent.Torrent) {
	a.mu.Lock()
	hash := t.InfoHash().HexString()
	delete(a.paused, hash)
	if record, ok := a.records[hash]; ok {
		record.Paused = false
		if record.AddedAt == 0 {
			record.AddedAt = time.Now().UnixNano()
		}
		a.records[hash] = record
	}
	if t.Info() == nil {
		a.queueReasons[hash] = "Fetching metadata"
	}
	a.mu.Unlock()
	a.saveTorrentRecords()
	a.scheduleDownloads()
}

func (a *app) pause(t *torrent.Torrent) {
	a.mu.Lock()
	hash := t.InfoHash().HexString()
	a.paused[hash] = true
	delete(a.activeDownloads, hash)
	a.queueReasons[hash] = "Paused"
	if record, ok := a.records[hash]; ok {
		record.Paused = true
		a.records[hash] = record
	}
	a.mu.Unlock()
	a.saveTorrentRecords()
	if t.Info() != nil {
		for _, file := range t.Files() {
			file.SetPriority(torrent.PiecePriorityNone)
		}
	}
	a.scheduleDownloads()
}

func (a *app) removeTorrent(t *torrent.Torrent) {
	paths := make([]string, 0)
	if t.Info() != nil {
		for _, file := range t.Files() {
			paths = append(paths, file.Path(), file.Path()+".part")
		}
	}
	hash := t.InfoHash().HexString()
	t.Drop()
	a.mu.Lock()
	record := a.records[hash]
	delete(a.paused, hash)
	delete(a.activeDownloads, hash)
	delete(a.queueReasons, hash)
	delete(a.samples, hash)
	delete(a.records, hash)
	a.mu.Unlock()
	a.saveTorrentRecords()
	if record.MetaFile != "" {
		_ = os.Remove(record.MetaFile)
	}
	removeTorrentFiles(a.root, paths)
	a.scheduleDownloads()
}

func removeTorrentFiles(root string, relativePaths []string) {
	root, err := filepath.Abs(root)
	if err != nil {
		return
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	emptyDirs := make(map[string]struct{})
	for _, relative := range relativePaths {
		cleaned := filepath.Clean(filepath.FromSlash(relative))
		if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
			continue
		}
		target := filepath.Join(root, cleaned)
		relativeTarget, err := filepath.Rel(root, target)
		if err != nil || relativeTarget == ".." || strings.HasPrefix(relativeTarget, ".."+string(os.PathSeparator)) {
			continue
		}
		parentReal, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			continue
		}
		relativeParent, err := filepath.Rel(rootReal, parentReal)
		if err != nil || relativeParent == ".." || strings.HasPrefix(relativeParent, ".."+string(os.PathSeparator)) {
			continue
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			continue
		}
		for dir := filepath.Dir(target); dir != root; dir = filepath.Dir(dir) {
			relativeDir, err := filepath.Rel(root, dir)
			if err != nil || relativeDir == "." || relativeDir == ".." || strings.HasPrefix(relativeDir, ".."+string(os.PathSeparator)) {
				break
			}
			emptyDirs[dir] = struct{}{}
		}
	}
	dirs := make([]string, 0, len(emptyDirs))
	for dir := range emptyDirs {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(os.PathSeparator)) > strings.Count(dirs[j], string(os.PathSeparator))
	})
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
}

func (a *app) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "all"
	}
	if query == "" {
		writeError(w, http.StatusBadRequest, "search query is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	results, err := search(ctx, provider, query)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, results)
}

func (a *app) find(hash string) (*torrent.Torrent, bool) {
	for _, t := range a.client.Torrents() {
		if strings.EqualFold(t.InfoHash().HexString(), hash) {
			return t, true
		}
	}
	return nil, false
}

func (a *app) view(t *torrent.Torrent) torrentView {
	name := "Fetching metadata..."
	var size, downloaded int64
	if t.Info() != nil {
		name = t.Name()
		size = t.Length()
		downloaded = t.BytesCompleted()
	}
	progress := float64(0)
	if size > 0 {
		progress = float64(downloaded) / float64(size) * 100
	} else if t.Info() != nil {
		progress = 100
	}
	hash := t.InfoHash().HexString()
	a.mu.RLock()
	isPaused := a.paused[hash] || a.records[hash].Paused
	isActive := a.activeDownloads[hash]
	queueReason := a.queueReasons[hash]
	limit := a.config.MaxDownloads
	addedAt := a.records[hash].AddedAt
	seeding := a.config.Seeding && a.config.Upload
	a.mu.RUnlock()
	if !validDownloadLimit(limit) {
		limit = 3
	}
	status := "Fetching metadata"
	if isPaused {
		status = "Paused"
	} else if t.Info() != nil {
		switch {
		case progress >= 100 && seeding:
			status = "Seeding"
		case progress >= 100:
			status = "Complete"
		case isActive:
			status = "Downloading"
		default:
			status = "Queued"
		}
	}
	if queueReason == "" && status == "Fetching metadata" {
		queueReason = "Fetching metadata"
	}
	if queueReason == "" && status == "Queued" {
		queueReason = "Waiting for an active slot"
	}
	added := time.Now().UTC().Format(time.RFC3339)
	if addedAt > 0 {
		added = time.Unix(0, addedAt).UTC().Format(time.RFC3339)
	}
	queuePosition := 0
	if status == "Queued" || status == "Fetching metadata" {
		queuePosition = a.queuePosition(hash)
	}
	stats := t.Stats()
	downloadSpeed, uploadSpeed := a.speeds(t)
	return torrentView{Hash: hash, Name: name, Size: size, Downloaded: downloaded, Progress: progress, Rate: stats.BytesReadUsefulData.Int64(), Peers: stats.ActivePeers, Status: status, AddedAt: added, Files: a.files(t), DownloadSpeed: downloadSpeed, UploadSpeed: uploadSpeed, QueuePosition: queuePosition, QueueReason: queueReason, DownloadLimit: limit}
}

func (a *app) speeds(t *torrent.Torrent) (int64, int64) {
	now := time.Now()
	stats := t.Stats()
	current := speedSample{at: now, downloaded: stats.BytesReadUsefulData.Int64(), uploaded: stats.BytesWrittenData.Int64()}
	a.mu.Lock()
	previous, ok := a.samples[t.InfoHash().HexString()]
	a.samples[t.InfoHash().HexString()] = current
	a.mu.Unlock()
	if !ok || current.at.Sub(previous.at) <= 0 {
		return 0, 0
	}
	seconds := current.at.Sub(previous.at).Seconds()
	return maxRate(current.downloaded-previous.downloaded, seconds), maxRate(current.uploaded-previous.uploaded, seconds)
}

func maxRate(bytes int64, seconds float64) int64 {
	if bytes <= 0 || seconds <= 0 {
		return 0
	}
	return int64(float64(bytes) / seconds)
}

func (a *app) files(t *torrent.Torrent) []fileView {
	if t.Info() == nil {
		return []fileView{}
	}
	items := make([]fileView, 0, len(t.Files()))
	for _, file := range t.Files() {
		items = append(items, fileView{Path: file.Path(), Size: file.Length(), Downloaded: file.BytesCompleted(), Complete: file.BytesCompleted() >= file.Length()})
	}
	return items
}

func (a *app) fileByPath(t *torrent.Torrent, path string) *torrent.File {
	for _, file := range t.Files() {
		if file.Path() == path {
			return file
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": message})
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
