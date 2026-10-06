package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveTorrentFilesPrunesOnlyEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	torrentDir := filepath.Join(root, "Sample Torrent")
	nestedDir := filepath.Join(torrentDir, "content")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.txt", "two.txt", "one.txt.part"} {
		if err := os.WriteFile(filepath.Join(nestedDir, name), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keepDir := filepath.Join(root, "Other Torrent")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keepFile := filepath.Join(keepDir, "keep.txt")
	if err := os.WriteFile(keepFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	removeTorrentFiles(root, []string{
		"Sample Torrent/content/one.txt",
		"Sample Torrent/content/two.txt",
		"Sample Torrent/content/one.txt.part",
	})

	if _, err := os.Stat(torrentDir); !os.IsNotExist(err) {
		t.Fatalf("expected empty torrent directory to be removed, stat error: %v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("expected unrelated file to remain: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected download root to remain: %v", err)
	}
}

func TestRemoveTorrentFilesRejectsPathsOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "downloads")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	removeTorrentFiles(root, []string{"../keep.txt"})

	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("expected outside file to remain: %v", err)
	}
}

func TestHasStorageCapacityReservesActiveAndSafetySpace(t *testing.T) {
	const mib = uint64(1 << 20)
	tests := []struct {
		name     string
		free     uint64
		reserved uint64
		size     int64
		want     bool
	}{
		{name: "fits with safety reserve", free: 200 * mib, reserved: 25 * mib, size: int64(100 * mib), want: true},
		{name: "active reservation leaves too little", free: 200 * mib, reserved: 40 * mib, size: int64(100 * mib), want: false},
		{name: "torrent leaves too little", free: 130 * mib, size: int64(100 * mib), want: false},
		{name: "torrent exceeds free space", free: 80 * mib, size: int64(100 * mib), want: false},
		{name: "negative size is invalid", free: 200 * mib, size: -1, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasStorageCapacity(test.free, test.reserved, test.size); got != test.want {
				t.Fatalf("hasStorageCapacity(%d, %d, %d) = %v, want %v", test.free, test.reserved, test.size, got, test.want)
			}
		})
	}
}

func TestHandleSettingsAppliesDownloadLimitWithoutRestart(t *testing.T) {
	t.Setenv("STATE_DIR", t.TempDir())
	downloadPath := t.TempDir()
	app := &app{root: downloadPath, config: settings{DownloadPath: downloadPath, MaxDownloads: 3}}
	body, err := json.Marshal(settings{DownloadPath: downloadPath, MaxDownloads: 5})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	response := httptest.NewRecorder()

	app.handleSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var result struct {
		Settings        settings `json:"settings"`
		RestartRequired bool     `json:"restartRequired"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Settings.MaxDownloads != 5 || app.config.MaxDownloads != 5 {
		t.Fatalf("expected download limit 5 to be applied, response=%d config=%d", result.Settings.MaxDownloads, app.config.MaxDownloads)
	}
	if result.RestartRequired {
		t.Fatal("changing the download limit should not require a restart")
	}
}

func TestHandleSettingsAllowsUnlimitedDownloads(t *testing.T) {
	t.Setenv("STATE_DIR", t.TempDir())
	downloadPath := t.TempDir()
	app := &app{root: downloadPath, config: settings{DownloadPath: downloadPath, MaxDownloads: 3}}
	body, err := json.Marshal(settings{DownloadPath: downloadPath, MaxDownloads: 0})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	response := httptest.NewRecorder()

	app.handleSettings(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if app.config.MaxDownloads != 0 {
		t.Fatalf("expected unlimited downloads, got limit %d", app.config.MaxDownloads)
	}
}

func TestHandleSettingsRejectsDownloadLimitOutsideSupportedRange(t *testing.T) {
	t.Setenv("STATE_DIR", t.TempDir())
	downloadPath := t.TempDir()
	app := &app{root: downloadPath, config: settings{DownloadPath: downloadPath, MaxDownloads: 3}}
	body, err := json.Marshal(settings{DownloadPath: downloadPath, MaxDownloads: 6})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	response := httptest.NewRecorder()

	app.handleSettings(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
	}
	if app.config.MaxDownloads != 3 {
		t.Fatalf("invalid download limit changed config to %d", app.config.MaxDownloads)
	}
}

func TestHandleProcessUsageReturnsProcessAndNetworkMetrics(t *testing.T) {
	app := &app{}
	request := httptest.NewRequest(http.MethodGet, "/api/process", nil)
	response := httptest.NewRecorder()

	app.handleProcessUsage(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	var usage processUsageView
	if err := json.Unmarshal(response.Body.Bytes(), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.MemoryBytes == 0 || usage.UpdatedAt == "" {
		t.Fatalf("expected process memory and update time, got %+v", usage)
	}
}
