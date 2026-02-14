package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

// --- slugifyInvariantID ---

func TestSlugifyInvariantID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Rule", "my-rule"},
		{"No TODO lists!", "no-todo-lists"},
		{"  Leading and trailing  ", "leading-and-trailing"},
		{"UPPERCASE", "uppercase"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"special @#$ chars", "special-chars"},
		{"", ""},
		{"---", ""},
		{"already-slugged", "already-slugged"},
		{"CamelCase Rule", "camelcase-rule"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugifyInvariantID(tt.input)
			if got != tt.want {
				t.Errorf("slugifyInvariantID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- readContentFlag ---

func TestReadContentFlag_Literal(t *testing.T) {
	got, err := readContentFlag("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestReadContentFlag_Stdin(t *testing.T) {
	// Create a temporary file to use as stdin
	tmp, err := os.CreateTemp("", "stdin-test")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("from stdin"); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatalf("seeking temp file: %v", err)
	}

	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()
	os.Stdin = tmp

	got, err := readContentFlag("-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from stdin" {
		t.Errorf("got %q, want %q", got, "from stdin")
	}
}

// --- Test helpers ---

// activePolicyJSON returns a minimal active policy response with the given invariants and roles.
func activePolicyJSON(policyID string, invariants []client.PolicyInvariant, roles map[string]client.PolicyRolePlaybook) string {
	resp := client.ActivePolicyResponse{
		PolicyID:   policyID,
		ProjectID:  "proj-test",
		Version:    1,
		UpdatedAt:  "2026-02-14T12:00:00Z",
		Invariants: invariants,
		Roles:      roles,
	}
	data, _ := json.Marshal(resp)
	return string(data)
}

// policyMutationServer creates a test server that:
// - Returns the given active policy on GET /v1/policies/active
// - Captures the POST /v1/policies body
// - Returns success on POST /v1/policies/{id}/activate
func policyMutationServer(t *testing.T, activePolicyBody string, capturedCreate *client.CreatePolicyRequest, activateCalled *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/policies/active":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(activePolicyBody))

		case r.Method == "POST" && r.URL.Path == "/v1/policies":
			if capturedCreate != nil {
				var req client.CreatePolicyRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decoding create request: %v", err)
				}
				*capturedCreate = req
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"policy_id":"pol-new","project_id":"proj-test","version":2,"created":true}`))

		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/activate"):
			if activateCalled != nil {
				*activateCalled = true
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"activated":true,"active_policy_id":"pol-new"}`))

		default:
			http.NotFound(w, r)
		}
	}))
}

// --- Add invariant ---

func TestPolicyAdd_Invariant(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", []client.PolicyInvariant{}, map[string]client.PolicyRolePlaybook{
		"developer": {Title: "Developer", PlaybookMD: "Dev playbook"},
	})

	var captured client.CreatePolicyRequest
	var activated bool
	server := policyMutationServer(t, body, &captured, &activated)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyAddTitle = "No TODO Lists"
	policyAddGuidance = "Use beads instead."
	policyAddInvariant = true
	policyAddRole = false
	defer func() {
		policyAddTitle = ""
		policyAddGuidance = ""
		policyAddInvariant = false
	}()

	err := addInvariant(cfg)
	if err != nil {
		t.Fatalf("addInvariant: %v", err)
	}

	if !activated {
		t.Error("expected activate to be called")
	}
	if captured.BasePolicyID != "pol-1" {
		t.Errorf("expected base_policy_id=pol-1, got %q", captured.BasePolicyID)
	}
	if len(captured.Bundle.Invariants) != 1 {
		t.Fatalf("expected 1 invariant, got %d", len(captured.Bundle.Invariants))
	}
	inv := captured.Bundle.Invariants[0]
	if inv.ID != "no-todo-lists" {
		t.Errorf("expected invariant ID 'no-todo-lists', got %q", inv.ID)
	}
	if inv.Title != "No TODO Lists" {
		t.Errorf("expected title 'No TODO Lists', got %q", inv.Title)
	}
	if inv.BodyMD != "Use beads instead." {
		t.Errorf("expected body 'Use beads instead.', got %q", inv.BodyMD)
	}
}

// --- Add role ---

func TestPolicyAdd_Role(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", []client.PolicyInvariant{
		{ID: "rule-1", Title: "Rule 1", BodyMD: "Body"},
	}, map[string]client.PolicyRolePlaybook{})

	var captured client.CreatePolicyRequest
	var activated bool
	server := policyMutationServer(t, body, &captured, &activated)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyAddName = "reviewer"
	policyAddPlaybook = "Review all PRs."
	policyAddInvariant = false
	policyAddRole = true
	defer func() {
		policyAddName = ""
		policyAddPlaybook = ""
		policyAddRole = false
	}()

	err := addRole(cfg)
	if err != nil {
		t.Fatalf("addRole: %v", err)
	}

	if !activated {
		t.Error("expected activate to be called")
	}
	if len(captured.Bundle.Invariants) != 1 {
		t.Error("expected existing invariant to be preserved")
	}
	playbook, ok := captured.Bundle.Roles["reviewer"]
	if !ok {
		t.Fatalf("expected role 'reviewer' in bundle, got roles: %v", captured.Bundle.Roles)
	}
	if playbook.PlaybookMD != "Review all PRs." {
		t.Errorf("expected playbook 'Review all PRs.', got %q", playbook.PlaybookMD)
	}
}

// --- Edit invariant ---

func TestPolicyEdit_Invariant(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", []client.PolicyInvariant{
		{ID: "my-rule", Title: "My Rule", BodyMD: "Old guidance"},
	}, map[string]client.PolicyRolePlaybook{})

	var captured client.CreatePolicyRequest
	server := policyMutationServer(t, body, &captured, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyEditInvariant = "my-rule"
	policyEditGuidance = "Updated guidance"
	policyEditTitle = "New Title"
	defer func() {
		policyEditInvariant = ""
		policyEditGuidance = ""
		policyEditTitle = ""
	}()

	err := editInvariant(cfg)
	if err != nil {
		t.Fatalf("editInvariant: %v", err)
	}

	if len(captured.Bundle.Invariants) != 1 {
		t.Fatalf("expected 1 invariant, got %d", len(captured.Bundle.Invariants))
	}
	inv := captured.Bundle.Invariants[0]
	if inv.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", inv.Title)
	}
	if inv.BodyMD != "Updated guidance" {
		t.Errorf("expected body 'Updated guidance', got %q", inv.BodyMD)
	}
}

// --- Edit role ---

func TestPolicyEdit_Role(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", nil, map[string]client.PolicyRolePlaybook{
		"reviewer": {Title: "Reviewer", PlaybookMD: "Old playbook"},
	})

	var captured client.CreatePolicyRequest
	server := policyMutationServer(t, body, &captured, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyEditRole = "reviewer"
	policyEditPlaybook = "Updated playbook"
	defer func() {
		policyEditRole = ""
		policyEditPlaybook = ""
	}()

	err := editRole(cfg)
	if err != nil {
		t.Fatalf("editRole: %v", err)
	}

	playbook, ok := captured.Bundle.Roles["reviewer"]
	if !ok {
		t.Fatalf("expected role 'reviewer' in bundle")
	}
	if playbook.PlaybookMD != "Updated playbook" {
		t.Errorf("expected 'Updated playbook', got %q", playbook.PlaybookMD)
	}
}

// --- Delete invariant ---

func TestPolicyDelete_Invariant(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", []client.PolicyInvariant{
		{ID: "keep-this", Title: "Keep This", BodyMD: "Keep"},
		{ID: "delete-me", Title: "Delete Me", BodyMD: "Gone"},
	}, map[string]client.PolicyRolePlaybook{})

	var captured client.CreatePolicyRequest
	server := policyMutationServer(t, body, &captured, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyDeleteInvariant = "delete-me"
	defer func() { policyDeleteInvariant = "" }()

	err := deleteInvariant(cfg)
	if err != nil {
		t.Fatalf("deleteInvariant: %v", err)
	}

	if len(captured.Bundle.Invariants) != 1 {
		t.Fatalf("expected 1 invariant remaining, got %d", len(captured.Bundle.Invariants))
	}
	if captured.Bundle.Invariants[0].ID != "keep-this" {
		t.Errorf("expected 'keep-this' to remain, got %q", captured.Bundle.Invariants[0].ID)
	}
}

// --- Delete role ---

func TestPolicyDelete_Role(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", nil, map[string]client.PolicyRolePlaybook{
		"reviewer":  {Title: "Reviewer", PlaybookMD: "Review"},
		"developer": {Title: "Developer", PlaybookMD: "Dev"},
	})

	var captured client.CreatePolicyRequest
	server := policyMutationServer(t, body, &captured, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyDeleteRole = "reviewer"
	defer func() { policyDeleteRole = "" }()

	err := deleteRole(cfg)
	if err != nil {
		t.Fatalf("deleteRole: %v", err)
	}

	if _, ok := captured.Bundle.Roles["reviewer"]; ok {
		t.Error("expected 'reviewer' to be deleted")
	}
	if _, ok := captured.Bundle.Roles["developer"]; !ok {
		t.Error("expected 'developer' to remain")
	}
}

// --- Duplicate invariant on add ---

func TestPolicyAdd_DuplicateInvariant(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", []client.PolicyInvariant{
		{ID: "my-rule", Title: "My Rule", BodyMD: "Existing"},
	}, map[string]client.PolicyRolePlaybook{})

	server := policyMutationServer(t, body, nil, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyAddTitle = "My Rule"
	policyAddGuidance = "New guidance"
	policyAddInvariant = true
	policyAddRole = false
	defer func() {
		policyAddTitle = ""
		policyAddGuidance = ""
		policyAddInvariant = false
	}()

	err := addInvariant(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate invariant")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

// --- Edit nonexistent invariant ---

func TestPolicyEdit_InvariantNotFound(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", []client.PolicyInvariant{
		{ID: "rule-a", Title: "Rule A", BodyMD: "A"},
		{ID: "rule-b", Title: "Rule B", BodyMD: "B"},
	}, map[string]client.PolicyRolePlaybook{})

	server := policyMutationServer(t, body, nil, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyEditInvariant = "nonexistent"
	policyEditGuidance = "Updated"
	defer func() {
		policyEditInvariant = ""
		policyEditGuidance = ""
	}()

	err := editInvariant(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent invariant")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rule-a") || !strings.Contains(err.Error(), "rule-b") {
		t.Errorf("expected available IDs in error, got: %v", err)
	}
}

// --- Delete nonexistent role ---

func TestPolicyDelete_RoleNotFound(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test")

	body := activePolicyJSON("pol-1", nil, map[string]client.PolicyRolePlaybook{
		"developer": {Title: "Developer", PlaybookMD: "Dev"},
	})

	server := policyMutationServer(t, body, nil, nil)
	defer server.Close()

	cfg := &config.Config{BeadhubURL: server.URL}
	policyDeleteRole = "nonexistent"
	defer func() { policyDeleteRole = "" }()

	err := deleteRole(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent role")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "developer") {
		t.Errorf("expected available role names in error, got: %v", err)
	}
}

// --- Cache invalidation ---

func TestInvalidatePolicyCache(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".beadhub-cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create some cache files
	files := []string{
		"policy-active.json",
		"policy-active-only-selected-developer.json",
		"policy-active-only-selected-reviewer.json",
		"other-file.json", // should not be deleted
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(cacheDir, f), []byte("{}"), 0600); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	invalidatePolicyCache(root)

	// Policy cache files should be gone
	for _, f := range files[:3] {
		if _, err := os.Stat(filepath.Join(cacheDir, f)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted", f)
		}
	}

	// other-file.json should remain
	if _, err := os.Stat(filepath.Join(cacheDir, "other-file.json")); err != nil {
		t.Errorf("expected other-file.json to remain, got: %v", err)
	}
}
