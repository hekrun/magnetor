package main

import (
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
