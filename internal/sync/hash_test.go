package sync

import (
	"strings"
	"testing"
)

func TestComputeIssueHash_Deterministic(t *testing.T) {
	// Same content should produce same hash
	issue1 := []byte(`{"id":"bd-1","title":"Test","status":"open"}`)
	issue2 := []byte(`{"id":"bd-1","title":"Test","status":"open"}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash != hash2.Hash {
		t.Errorf("Expected same hash, got %s and %s", hash1.Hash, hash2.Hash)
	}
}

func TestComputeIssueHash_KeyOrderIndependent(t *testing.T) {
	// Different key order should produce same hash
	issue1 := []byte(`{"id":"bd-1","status":"open","title":"Test"}`)
	issue2 := []byte(`{"title":"Test","id":"bd-1","status":"open"}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash != hash2.Hash {
		t.Errorf("Expected same hash for different key order, got %s and %s", hash1.Hash, hash2.Hash)
	}

	if hash1.ID != "bd-1" || hash2.ID != "bd-1" {
		t.Errorf("Expected ID bd-1, got %s and %s", hash1.ID, hash2.ID)
	}
}

func TestComputeIssueHash_DifferentContentDifferentHash(t *testing.T) {
	issue1 := []byte(`{"id":"bd-1","title":"Test","status":"open"}`)
	issue2 := []byte(`{"id":"bd-1","title":"Test","status":"closed"}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash == hash2.Hash {
		t.Errorf("Expected different hash for different content, got same: %s", hash1.Hash)
	}
}

func TestComputeIssueHash_NestedObjects(t *testing.T) {
	// Nested objects should also have sorted keys
	issue1 := []byte(`{"id":"bd-1","deps":{"a":1,"b":2}}`)
	issue2 := []byte(`{"id":"bd-1","deps":{"b":2,"a":1}}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash != hash2.Hash {
		t.Errorf("Expected same hash for nested objects with different key order, got %s and %s", hash1.Hash, hash2.Hash)
	}
}

func TestComputeIssueHash_Arrays(t *testing.T) {
	// Array order should be preserved (not sorted)
	issue1 := []byte(`{"id":"bd-1","tags":["a","b","c"]}`)
	issue2 := []byte(`{"id":"bd-1","tags":["c","b","a"]}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash == hash2.Hash {
		t.Errorf("Expected different hash for different array order, got same: %s", hash1.Hash)
	}
}

func TestComputeIssueHash_NoID(t *testing.T) {
	issue := []byte(`{"title":"Test","status":"open"}`)

	hash, err := ComputeIssueHash(issue)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash.ID != "" {
		t.Errorf("Expected empty ID, got %s", hash.ID)
	}
}

func TestComputeIssueHashes(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First","status":"open"}
{"id":"bd-2","title":"Second","status":"closed"}
{"id":"bd-3","title":"Third","status":"in_progress"}`)

	hashes, err := ComputeIssueHashes(jsonl)
	if err != nil {
		t.Fatalf("ComputeIssueHashes failed: %v", err)
	}

	if len(hashes) != 3 {
		t.Errorf("Expected 3 hashes, got %d", len(hashes))
	}

	if _, exists := hashes["bd-1"]; !exists {
		t.Error("Expected hash for bd-1")
	}
	if _, exists := hashes["bd-2"]; !exists {
		t.Error("Expected hash for bd-2")
	}
	if _, exists := hashes["bd-3"]; !exists {
		t.Error("Expected hash for bd-3")
	}
}

func TestComputeIssueHashes_EmptyLines(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}

{"id":"bd-2","title":"Second"}
`)

	hashes, err := ComputeIssueHashes(jsonl)
	if err != nil {
		t.Fatalf("ComputeIssueHashes failed: %v", err)
	}

	if len(hashes) != 2 {
		t.Errorf("Expected 2 hashes, got %d", len(hashes))
	}
}

func TestComputeIssueHashes_WindowsLineEndings(t *testing.T) {
	jsonl := []byte("{\"id\":\"bd-1\",\"title\":\"First\"}\r\n{\"id\":\"bd-2\",\"title\":\"Second\"}\r\n")

	hashes, err := ComputeIssueHashes(jsonl)
	if err != nil {
		t.Fatalf("ComputeIssueHashes failed: %v", err)
	}

	if len(hashes) != 2 {
		t.Errorf("Expected 2 hashes, got %d", len(hashes))
	}
}

func TestFindChangedIssues(t *testing.T) {
	current := map[string]string{
		"bd-1": "hash1",
		"bd-2": "hash2-updated",
		"bd-3": "hash3-new",
	}

	lastSynced := map[string]string{
		"bd-1": "hash1",
		"bd-2": "hash2",
	}

	changed := FindChangedIssues(current, lastSynced)

	if len(changed) != 2 {
		t.Errorf("Expected 2 changed issues, got %d", len(changed))
	}

	// Check that bd-2 (updated) and bd-3 (new) are in changed
	hasbd2, hasbd3 := false, false
	for _, id := range changed {
		if id == "bd-2" {
			hasbd2 = true
		}
		if id == "bd-3" {
			hasbd3 = true
		}
	}

	if !hasbd2 {
		t.Error("Expected bd-2 (updated) to be in changed")
	}
	if !hasbd3 {
		t.Error("Expected bd-3 (new) to be in changed")
	}
}

func TestFindDeletedIssues(t *testing.T) {
	current := map[string]string{
		"bd-1": "hash1",
	}

	lastSynced := map[string]string{
		"bd-1": "hash1",
		"bd-2": "hash2",
		"bd-3": "hash3",
	}

	deleted := FindDeletedIssues(current, lastSynced)

	if len(deleted) != 2 {
		t.Errorf("Expected 2 deleted issues, got %d", len(deleted))
	}

	// Check that bd-2 and bd-3 are in deleted
	hasbd2, hasbd3 := false, false
	for _, id := range deleted {
		if id == "bd-2" {
			hasbd2 = true
		}
		if id == "bd-3" {
			hasbd3 = true
		}
	}

	if !hasbd2 {
		t.Error("Expected bd-2 to be in deleted")
	}
	if !hasbd3 {
		t.Error("Expected bd-3 to be in deleted")
	}
}

func TestFindChangedIssues_Empty(t *testing.T) {
	current := map[string]string{
		"bd-1": "hash1",
	}

	lastSynced := map[string]string{
		"bd-1": "hash1",
	}

	changed := FindChangedIssues(current, lastSynced)

	if len(changed) != 0 {
		t.Errorf("Expected 0 changed issues, got %d", len(changed))
	}
}

func TestFindChangedIssues_FirstSync(t *testing.T) {
	current := map[string]string{
		"bd-1": "hash1",
		"bd-2": "hash2",
	}

	lastSynced := map[string]string{}

	changed := FindChangedIssues(current, lastSynced)

	if len(changed) != 2 {
		t.Errorf("Expected 2 changed issues (all new), got %d", len(changed))
	}
}

func TestSplitJSONL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"single line", `{"id":"1"}`, 1},
		{"two lines", "{\"id\":\"1\"}\n{\"id\":\"2\"}", 2},
		{"trailing newline", "{\"id\":\"1\"}\n{\"id\":\"2\"}\n", 2},
		{"empty lines", "{\"id\":\"1\"}\n\n{\"id\":\"2\"}", 2},
		{"windows line endings", "{\"id\":\"1\"}\r\n{\"id\":\"2\"}\r\n", 2},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := splitJSONL([]byte(tt.input))
			if len(lines) != tt.expected {
				t.Errorf("Expected %d lines, got %d", tt.expected, len(lines))
			}
		})
	}
}

func TestComputeIssueHash_Unicode(t *testing.T) {
	// Go's JSON encoder normalizes Unicode escapes, verify this
	issue1 := []byte(`{"id":"bd-1","title":"Test \u0041"}`) // \u0041 = 'A'
	issue2 := []byte(`{"id":"bd-1","title":"Test A"}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash != hash2.Hash {
		t.Errorf("Expected same hash for escaped and unescaped Unicode, got %s and %s", hash1.Hash, hash2.Hash)
	}
}

func TestComputeIssueHash_DeepNesting(t *testing.T) {
	// Verify deeply nested structures have sorted keys at all levels
	issue1 := []byte(`{"id":"bd-1","meta":{"tags":[{"name":"bug","priority":1}]}}`)
	issue2 := []byte(`{"id":"bd-1","meta":{"tags":[{"priority":1,"name":"bug"}]}}`)

	hash1, err := ComputeIssueHash(issue1)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	hash2, err := ComputeIssueHash(issue2)
	if err != nil {
		t.Fatalf("ComputeIssueHash failed: %v", err)
	}

	if hash1.Hash != hash2.Hash {
		t.Errorf("Expected same hash for deeply nested objects with different key order, got %s and %s", hash1.Hash, hash2.Hash)
	}
}

func TestComputeIssueHashes_InvalidJSON(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}
{this is not valid json}
{"id":"bd-3","title":"Third"}`)

	_, err := ComputeIssueHashes(jsonl)
	if err == nil {
		t.Error("Expected error for invalid JSON line")
	}
	// Verify error message is helpful
	if !strings.Contains(err.Error(), "invalid character") {
		t.Errorf("Expected JSON parse error, got: %v", err)
	}
}

func TestComputeIssueHashes_SkipsIssuesWithoutID(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}
{"title":"No ID issue"}
{"id":"bd-3","title":"Third"}`)

	hashes, err := ComputeIssueHashes(jsonl)
	if err != nil {
		t.Fatalf("ComputeIssueHashes failed: %v", err)
	}

	// Should have 2 hashes (bd-1 and bd-3), skipping the one without ID
	if len(hashes) != 2 {
		t.Errorf("Expected 2 hashes, got %d", len(hashes))
	}

	if _, exists := hashes["bd-1"]; !exists {
		t.Error("Expected hash for bd-1")
	}
	if _, exists := hashes["bd-3"]; !exists {
		t.Error("Expected hash for bd-3")
	}
}

func TestExtractIssuesByID(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}
{"id":"bd-2","title":"Second"}
{"id":"bd-3","title":"Third"}`)

	result, err := ExtractIssuesByID(jsonl, []string{"bd-1", "bd-3"})
	if err != nil {
		t.Fatalf("ExtractIssuesByID failed: %v", err)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
	}

	// Verify content
	if !strings.Contains(result, `"id":"bd-1"`) {
		t.Error("Expected bd-1 in result")
	}
	if !strings.Contains(result, `"id":"bd-3"`) {
		t.Error("Expected bd-3 in result")
	}
	if strings.Contains(result, `"id":"bd-2"`) {
		t.Error("bd-2 should not be in result")
	}
}

func TestExtractIssuesByID_Empty(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}`)

	result, err := ExtractIssuesByID(jsonl, []string{})
	if err != nil {
		t.Fatalf("ExtractIssuesByID failed: %v", err)
	}

	if result != "" {
		t.Errorf("Expected empty result, got %s", result)
	}
}

func TestExtractIssuesByID_NoMatch(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}`)

	result, err := ExtractIssuesByID(jsonl, []string{"bd-999"})
	if err != nil {
		t.Fatalf("ExtractIssuesByID failed: %v", err)
	}

	if result != "" {
		t.Errorf("Expected empty result, got %s", result)
	}
}

func TestParseIssueIDs(t *testing.T) {
	jsonl := []byte(`{"id":"bd-1","title":"First"}
{"id":"bd-2","title":"Second"}
{"title":"No ID"}
{"id":"bd-3","title":"Third"}`)

	ids, err := ParseIssueIDs(jsonl)
	if err != nil {
		t.Fatalf("ParseIssueIDs failed: %v", err)
	}

	if len(ids) != 3 {
		t.Errorf("Expected 3 IDs, got %d", len(ids))
	}

	// Check IDs are present
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	if !idSet["bd-1"] || !idSet["bd-2"] || !idSet["bd-3"] {
		t.Errorf("Missing expected IDs, got %v", ids)
	}
}
