package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadState_FileNotExists(t *testing.T) {
	// Non-existent file should return empty state (full sync fallback)
	state, err := LoadState("/nonexistent/path/sync-state.json")
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if state == nil {
		t.Fatal("Expected non-nil state")
	}

	if len(state.IssueHashes) != 0 {
		t.Errorf("Expected empty hashes, got %d", len(state.IssueHashes))
	}

	if !state.LastSync.IsZero() {
		t.Errorf("Expected zero LastSync, got %v", state.LastSync)
	}
}

func TestLoadState_CorruptFile(t *testing.T) {
	// Create temp file with corrupt JSON
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sync-state.json")

	if err := os.WriteFile(path, []byte("this is not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Corrupt file should return empty state (full sync fallback)
	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(state.IssueHashes) != 0 {
		t.Errorf("Expected empty hashes for corrupt file, got %d", len(state.IssueHashes))
	}
}

func TestLoadState_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sync-state.json")

	content := `{
  "last_sync": "2025-01-01T12:00:00Z",
  "issue_hashes": {
    "bd-1": "hash1",
    "bd-2": "hash2"
  }
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(state.IssueHashes) != 2 {
		t.Errorf("Expected 2 hashes, got %d", len(state.IssueHashes))
	}

	if state.IssueHashes["bd-1"] != "hash1" {
		t.Errorf("Expected hash1, got %s", state.IssueHashes["bd-1"])
	}

	expected := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	if !state.LastSync.Equal(expected) {
		t.Errorf("Expected LastSync %v, got %v", expected, state.LastSync)
	}
}

func TestLoadState_NullHashes(t *testing.T) {
	// JSON with null issue_hashes should be handled
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sync-state.json")

	content := `{"last_sync": "2025-01-01T12:00:00Z", "issue_hashes": null}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	// Should initialize empty map
	if state.IssueHashes == nil {
		t.Error("Expected non-nil IssueHashes map")
	}
}

func TestSaveState(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "subdir", "sync-state.json")

	state := &SyncState{
		LastSync: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		IssueHashes: map[string]string{
			"bd-1": "hash1",
			"bd-2": "hash2",
		},
	}

	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify file exists and can be loaded
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if len(loaded.IssueHashes) != 2 {
		t.Errorf("Expected 2 hashes, got %d", len(loaded.IssueHashes))
	}

	if !loaded.LastSync.Equal(state.LastSync) {
		t.Errorf("LastSync mismatch: expected %v, got %v", state.LastSync, loaded.LastSync)
	}
}

func TestSaveState_Atomic(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sync-state.json")

	state := &SyncState{
		LastSync: time.Now().UTC(),
		IssueHashes: map[string]string{
			"bd-1": "hash1",
		},
	}

	// Save should not leave .tmp file
	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Expected .tmp file to be removed after atomic save")
	}
}

func TestUpdateState(t *testing.T) {
	state := &SyncState{
		IssueHashes: map[string]string{
			"bd-1": "old-hash",
		},
	}

	newHashes := map[string]string{
		"bd-1": "new-hash",
		"bd-2": "new-hash-2",
	}

	UpdateState(state, newHashes)

	if len(state.IssueHashes) != 2 {
		t.Errorf("Expected 2 hashes, got %d", len(state.IssueHashes))
	}

	if state.IssueHashes["bd-1"] != "new-hash" {
		t.Errorf("Expected new-hash, got %s", state.IssueHashes["bd-1"])
	}

	if state.LastSync.IsZero() {
		t.Error("Expected LastSync to be set")
	}
}

func TestNeedsFullSync(t *testing.T) {
	tests := []struct {
		name     string
		hashes   map[string]string
		expected bool
	}{
		{"empty hashes", map[string]string{}, true},
		{"nil hashes", nil, true},
		{"has hashes", map[string]string{"bd-1": "hash"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &SyncState{IssueHashes: tt.hashes}
			if got := NeedsFullSync(state); got != tt.expected {
				t.Errorf("NeedsFullSync() = %v, want %v", got, tt.expected)
			}
		})
	}
}
