// Package sync provides incremental sync functionality for bead issues.
//
// This package implements hash-based change detection to enable efficient
// incremental syncing. Instead of sending all issues on every sync, we:
// 1. Compute SHA256 hash for each issue (sorted keys for determinism)
// 2. Compare with last-synced hashes
// 3. Only send issues where hash changed
//
// Edge cases:
//   - Issues without 'id' field are silently skipped (can't be tracked for sync)
//   - Invalid JSON lines cause the entire operation to fail
//   - Both Windows (\r\n) and Unix (\n) line endings are supported
//   - Hash is deterministic: different JSON key orders produce identical hashes
//   - Array element order is preserved (different order = different hash)
//
// Deleted issue detection is provided by FindDeletedIssues.
package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// HashVersion is the version prefix for issue hashes.
// Increment this when changing the hashing algorithm to invalidate old sync state
// and trigger a full sync, ensuring clients don't miss changes.
const HashVersion = "v1"

// IssueHash represents a computed hash for a bead issue.
type IssueHash struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

// ComputeIssueHash computes a deterministic SHA256 hash for a single issue.
// The issue JSON is re-marshaled with sorted keys for determinism.
func ComputeIssueHash(issueJSON []byte) (IssueHash, error) {
	// Parse JSON into a map for re-marshaling with sorted keys
	var issueMap map[string]any
	if err := json.Unmarshal(issueJSON, &issueMap); err != nil {
		return IssueHash{}, err
	}

	// Get the issue ID
	id, _ := issueMap["id"].(string)
	if id == "" {
		return IssueHash{}, nil // Skip issues without ID
	}

	// Marshal with sorted keys for determinism
	canonical, err := marshalSorted(issueMap)
	if err != nil {
		return IssueHash{}, err
	}

	// Compute SHA256 hash with version prefix for future algorithm changes
	hash := sha256.Sum256(canonical)
	return IssueHash{
		ID:   id,
		Hash: fmt.Sprintf("%s:%s", HashVersion, hex.EncodeToString(hash[:])),
	}, nil
}

// ComputeIssueHashes computes hashes for all issues in a JSONL content.
// Returns a map from issue ID to hash.
func ComputeIssueHashes(jsonlContent []byte) (map[string]string, error) {
	hashes := make(map[string]string)

	// Split by newlines and process each line
	lines := splitJSONL(jsonlContent)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		issueHash, err := ComputeIssueHash(line)
		if err != nil {
			return nil, err
		}
		if issueHash.ID != "" {
			hashes[issueHash.ID] = issueHash.Hash
		}
	}

	return hashes, nil
}

// FindChangedIssues compares current hashes with last-synced hashes
// and returns the IDs of issues that have changed or are new.
func FindChangedIssues(current, lastSynced map[string]string) []string {
	var changed []string

	for id, currentHash := range current {
		lastHash, exists := lastSynced[id]
		if !exists || lastHash != currentHash {
			changed = append(changed, id)
		}
	}

	// Sort for deterministic output
	sort.Strings(changed)
	return changed
}

// FindDeletedIssues returns IDs of issues that existed in lastSynced
// but are no longer present in current.
func FindDeletedIssues(current, lastSynced map[string]string) []string {
	var deleted []string

	for id := range lastSynced {
		if _, exists := current[id]; !exists {
			deleted = append(deleted, id)
		}
	}

	// Sort for deterministic output
	sort.Strings(deleted)
	return deleted
}

// marshalSorted marshals a map to JSON with sorted keys at all levels.
func marshalSorted(v any) ([]byte, error) {
	normalized := normalizeValue(v)
	return json.Marshal(normalized)
}

// normalizeValue recursively normalizes a value for deterministic JSON output.
// Maps are converted to sorted key order, arrays are preserved.
func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return normalizeSortedMap(val)
	case []any:
		result := make([]any, len(val))
		for i, elem := range val {
			result[i] = normalizeValue(elem)
		}
		return result
	default:
		return v
	}
}

// normalizeSortedMap creates a sorted representation of a map for deterministic JSON.
type sortedMap struct {
	keys   []string
	values map[string]any
}

func (s sortedMap) MarshalJSON() ([]byte, error) {
	buf := []byte("{")
	for i, key := range s.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		valueJSON, err := json.Marshal(s.values[key])
		if err != nil {
			return nil, err
		}
		buf = append(buf, keyJSON...)
		buf = append(buf, ':')
		buf = append(buf, valueJSON...)
	}
	buf = append(buf, '}')
	return buf, nil
}

func normalizeSortedMap(m map[string]any) sortedMap {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	normalized := make(map[string]any, len(m))
	for _, k := range keys {
		normalized[k] = normalizeValue(m[k])
	}

	return sortedMap{keys: keys, values: normalized}
}

// splitJSONL splits JSONL content into individual JSON lines.
func splitJSONL(content []byte) [][]byte {
	var lines [][]byte
	var start int

	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			line := content[start:i]
			// Trim any trailing \r for Windows compatibility
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}

	// Handle last line without trailing newline
	if start < len(content) {
		line := content[start:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}

	return lines
}

// ExtractIssuesByID extracts issues matching the given IDs from JSONL content.
// Returns a JSONL string containing only the matching issues.
func ExtractIssuesByID(jsonlContent []byte, ids []string) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}

	// Build ID set for fast lookup
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	var result []string
	lines := splitJSONL(jsonlContent)

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		// Parse to get the ID
		var issueMap map[string]any
		if err := json.Unmarshal(line, &issueMap); err != nil {
			continue // Skip invalid lines
		}

		id, _ := issueMap["id"].(string)
		if id != "" && idSet[id] {
			result = append(result, string(line))
		}
	}

	return strings.Join(result, "\n"), nil
}

// ParseIssueIDs extracts all issue IDs from JSONL content.
func ParseIssueIDs(jsonlContent []byte) ([]string, error) {
	var ids []string
	lines := splitJSONL(jsonlContent)

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		var issueMap map[string]any
		if err := json.Unmarshal(line, &issueMap); err != nil {
			continue // Skip invalid lines
		}

		id, _ := issueMap["id"].(string)
		if id != "" {
			ids = append(ids, id)
		}
	}

	return ids, nil
}
