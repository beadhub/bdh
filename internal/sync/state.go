// Sync state management for incremental sync.
//
// IMPORTANT: Concurrent access is not supported. Running multiple bdh processes
// simultaneously (e.g., in separate terminals) may cause race conditions when
// updating the sync state file. The last writer wins, which may result in
// missed changes being detected as new on the next sync. This is a benign
// failure mode - it only causes unnecessary data transfer, not data loss.
// The system recovers automatically on the next sync.
package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// SyncState represents the persistent state for incremental sync.
type SyncState struct {
	// LastSync is the timestamp of the last successful sync.
	LastSync time.Time `json:"last_sync"`

	// ProtocolVersion is the sync protocol version last seen from the server.
	// When this differs from the server's required version, the client should
	// perform a full sync to backfill newly-supported fields.
	ProtocolVersion int `json:"protocol_version,omitempty"`

	// IssueHashes maps issue ID to its SHA256 hash at last sync.
	IssueHashes map[string]string `json:"issue_hashes"`
}

// LoadState loads sync state from file.
// Returns empty state (triggering full sync) if file doesn't exist or is corrupt.
func LoadState(path string) (*SyncState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No state file - return empty state for full sync
			return &SyncState{
				IssueHashes: make(map[string]string),
			}, nil
		}
		return nil, err
	}

	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupt file - return empty state for full sync
		return &SyncState{
			IssueHashes: make(map[string]string),
		}, nil
	}

	// Ensure map is initialized
	if state.IssueHashes == nil {
		state.IssueHashes = make(map[string]string)
	}

	return &state, nil
}

// SaveState saves sync state to file atomically.
// Uses write-rename pattern to prevent corruption.
func SaveState(path string, state *SyncState) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Marshal with indentation for human readability
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// Write to temp file first, then rename (atomic on most filesystems)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// UpdateState updates the sync state after a successful sync.
// Takes the new issue hashes and updates the timestamp.
func UpdateState(state *SyncState, newHashes map[string]string) {
	state.LastSync = time.Now().UTC()
	state.IssueHashes = newHashes
}

// NeedsFullSync returns true if a full sync is required.
// This happens when there's no prior state (empty hashes).
func NeedsFullSync(state *SyncState) bool {
	return len(state.IssueHashes) == 0
}
