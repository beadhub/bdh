package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

func TestFormatPolicyOutput_PlainDeterministicOrdering(t *testing.T) {
	result := &PolicyResult{
		Role: "coordinator",
		Policy: &client.ActivePolicyResponse{
			Version:   3,
			UpdatedAt: "2026-01-02T12:00:00Z",
			Invariants: []client.PolicyInvariant{
				{ID: "b", Title: "Mail-first communication", BodyMD: "Use mail by default."},
				{ID: "a", Title: "Use bdh for tracking", BodyMD: "No TODO lists."},
			},
			SelectedRole: &client.SelectedPolicyRole{
				Role:       "coordinator",
				Title:      "Coordinator",
				PlaybookMD: "- Triage ready work\n- Delegate beads",
			},
		},
	}

	out := formatPolicyOutput(result, false, "plain")

	if !strings.Contains(out, "Policy: v3") {
		t.Fatalf("expected policy header, got: %s", out)
	}
	if !strings.Contains(out, "Role: coordinator") {
		t.Fatalf("expected role line, got: %s", out)
	}

	idxTracking := strings.Index(out, "- Use bdh for tracking")
	idxMail := strings.Index(out, "- Mail-first communication")
	if idxTracking == -1 || idxMail == -1 {
		t.Fatalf("expected invariant titles, got: %s", out)
	}
	if idxMail > idxTracking {
		t.Fatalf("expected invariants sorted by title, got: %s", out)
	}
}

func TestFormatPolicyOutput_PlainShowsFullInvariantBodies(t *testing.T) {
	multiLineBody := "First line of the invariant.\n\nSecond paragraph with details.\n\n## Subsection\n\n- Item 1\n- Item 2"
	result := &PolicyResult{
		Role: "coordinator",
		Policy: &client.ActivePolicyResponse{
			Version:   3,
			UpdatedAt: "2026-01-02T12:00:00Z",
			Invariants: []client.PolicyInvariant{
				{ID: "a", Title: "Multi-line invariant", BodyMD: multiLineBody},
			},
			SelectedRole: &client.SelectedPolicyRole{
				Role:       "coordinator",
				Title:      "Coordinator",
				PlaybookMD: "Playbook content",
			},
		},
	}

	out := formatPolicyOutput(result, false, "plain")

	// Verify all lines of the body are present
	if !strings.Contains(out, "First line of the invariant.") {
		t.Fatalf("expected first line of body, got: %s", out)
	}
	if !strings.Contains(out, "Second paragraph with details.") {
		t.Fatalf("expected second paragraph, got: %s", out)
	}
	if !strings.Contains(out, "- Item 1") {
		t.Fatalf("expected list items, got: %s", out)
	}
	if !strings.Contains(out, "- Item 2") {
		t.Fatalf("expected list items, got: %s", out)
	}

	// Verify body is indented under the title
	if !strings.Contains(out, "    First line") {
		t.Fatalf("expected body to be indented with 4 spaces, got: %s", out)
	}
}

func TestFormatPolicyOutput_MarkdownShowsFullInvariantBodies(t *testing.T) {
	multiLineBody := "First line.\n\nSecond paragraph."
	result := &PolicyResult{
		Role: "reviewer",
		Policy: &client.ActivePolicyResponse{
			Version: 2,
			Invariants: []client.PolicyInvariant{
				{ID: "x", Title: "Test invariant", BodyMD: multiLineBody},
			},
			SelectedRole: &client.SelectedPolicyRole{
				Role:       "reviewer",
				PlaybookMD: "Review code",
			},
		},
	}

	out := formatPolicyOutput(result, false, "markdown")

	// Verify markdown uses H3 for invariant titles
	if !strings.Contains(out, "### Test invariant") {
		t.Fatalf("expected H3 header for invariant title, got: %s", out)
	}
	// Verify full body is present
	if !strings.Contains(out, "First line.") {
		t.Fatalf("expected first line, got: %s", out)
	}
	if !strings.Contains(out, "Second paragraph.") {
		t.Fatalf("expected second paragraph, got: %s", out)
	}
}

func TestFetchActivePolicyWithConfig_UnknownRoleListsAvailableRoles(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test123")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/policies/active" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer aw_sk_test123" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"missing Authorization"}`))
			return
		}

		role := r.URL.Query().Get("role")
		if role == "unknown-role" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"unknown role"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
  "policy_id": "pol-123",
  "project_id": "proj-456",
  "version": 3,
  "updated_at": "2026-01-02T12:00:00Z",
  "invariants": [],
  "roles": {
    "coordinator": {"title": "Coordinator", "playbook_md": "…"},
    "reviewer": {"title": "Reviewer", "playbook_md": "…"}
  }
}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		BeadhubURL: server.URL,
	}

	_, err := fetchActivePolicyWithConfig(cfg, "unknown-role", false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "available roles: coordinator, reviewer") {
		t.Fatalf("expected available roles in error, got: %v", err)
	}
}

func TestPolicyCache_ReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy-cache.json")

	cache := &policyCacheFile{
		CachedAt: "2026-01-02T12:00:00Z",
		ETag:     "etag-1",
		Policy: &client.ActivePolicyResponse{
			PolicyID: "pol-123",
			Version:  3,
		},
	}
	if err := writePolicyCache(path, cache); err != nil {
		t.Fatalf("writePolicyCache: %v", err)
	}
	got, err := readPolicyCache(path)
	if err != nil {
		t.Fatalf("readPolicyCache: %v", err)
	}
	if got == nil || got.Policy == nil || got.Policy.PolicyID != "pol-123" {
		t.Fatalf("unexpected cache read: %#v", got)
	}
	if got.ETag != "etag-1" {
		t.Fatalf("expected etag-1, got %q", got.ETag)
	}
}

func TestEnsurePolicyCacheDir_AddsGitExclude(t *testing.T) {
	root := t.TempDir()
	excludePath := filepath.Join(root, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(excludePath, []byte(""), 0600); err != nil {
		t.Fatalf("write exclude: %v", err)
	}

	if err := ensurePolicyCacheDir(root); err != nil {
		t.Fatalf("ensurePolicyCacheDir: %v", err)
	}
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(data), ".beadhub-cache/") {
		t.Fatalf("expected exclude to contain .beadhub-cache/, got:\n%s", string(data))
	}
}

func TestFetchActivePolicyCachedWithConfig_UsesCacheWhenServerDown(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test123")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"policy_id":"pol-123","project_id":"proj-456","version":3,"updated_at":"2026-01-02T12:00:00Z","invariants":[],"roles":{}}`))
	}))
	serverURL := server.URL
	server.Close()

	root := t.TempDir()
	cacheDir := filepath.Join(root, ".beadhub-cache")
	cachePath := filepath.Join(cacheDir, "policy-active.json")
	old := time.Now().Add(-2 * time.Minute).Format(time.RFC3339)
	if err := writePolicyCache(cachePath, &policyCacheFile{
		CachedAt: old,
		Policy: &client.ActivePolicyResponse{
			PolicyID:  "pol-123",
			Version:   3,
			UpdatedAt: "2026-01-02T12:00:00Z",
			Roles: map[string]client.PolicyRolePlaybook{
				"coordinator": {Title: "Coordinator", PlaybookMD: "…"},
			},
		},
	}); err != nil {
		t.Fatalf("writePolicyCache: %v", err)
	}

	cfg := &config.Config{
		BeadhubURL: serverURL,
	}
	result, err := fetchActivePolicyCachedWithConfig(cfg, "coordinator", false, root)
	if err != nil {
		t.Fatalf("fetchActivePolicyCachedWithConfig: %v", err)
	}
	if result.Cache == nil || result.Cache.Mode != "offline" || !result.Cache.Used || !result.Cache.Stale {
		t.Fatalf("expected offline stale cache usage, got: %#v", result.Cache)
	}
}

func TestFetchActivePolicyCachedWithConfig_UsesConditionalGET304(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test123")

	root := t.TempDir()
	cacheDir := filepath.Join(root, ".beadhub-cache")
	cachePath := filepath.Join(cacheDir, "policy-active.json")
	oldCachedAt := "2026-01-02T12:00:00Z"
	if err := writePolicyCache(cachePath, &policyCacheFile{
		CachedAt: oldCachedAt,
		ETag:     "etag-1",
		Policy: &client.ActivePolicyResponse{
			PolicyID:   "pol-123",
			ProjectID:  "proj-456",
			Version:    3,
			UpdatedAt:  "2026-01-02T12:00:00Z",
			Invariants: []client.PolicyInvariant{},
			Roles: map[string]client.PolicyRolePlaybook{
				"coordinator": {Title: "Coordinator", PlaybookMD: "…"},
			},
		},
	}); err != nil {
		t.Fatalf("writePolicyCache: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "etag-1" {
			w.Header().Set("ETag", "etag-1")
			w.WriteHeader(http.StatusNotModified)
			return
		}
		t.Fatalf("expected If-None-Match=etag-1, got %q", r.Header.Get("If-None-Match"))
	}))
	defer server.Close()

	cfg := &config.Config{
		BeadhubURL: server.URL,
	}
	result, err := fetchActivePolicyCachedWithConfig(cfg, "coordinator", false, root)
	if err != nil {
		t.Fatalf("fetchActivePolicyCachedWithConfig: %v", err)
	}
	if result.Cache == nil || result.Cache.Mode != "validated" || !result.Cache.Used {
		t.Fatalf("expected validated cache usage, got: %#v", result.Cache)
	}

	updated, err := readPolicyCache(cachePath)
	if err != nil {
		t.Fatalf("readPolicyCache: %v", err)
	}
	if updated == nil || updated.CachedAt == oldCachedAt {
		t.Fatalf("expected cached_at to be refreshed, got: %#v", updated)
	}
}

func TestFetchActivePolicyCachedWithConfig_UsesAccountWhenEnvMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmp, "aw-config.yaml"))
	t.Setenv("BEADHUB_API_KEY", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/policies/active" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer aw_sk_from_account" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"missing Authorization"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "policy_id": "pol-123",
  "project_id": "proj-456",
  "version": 3,
  "updated_at": "2026-01-02T12:00:00Z",
  "invariants": [],
  "roles": {
    "coordinator": {"title": "Coordinator", "playbook_md": "…"}
  }
}`))
	}))
	defer server.Close()

	wd := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(wd, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	serverName, err := awconfig.DeriveServerNameFromURL(server.URL)
	if err != nil {
		t.Fatalf("derive server name: %v", err)
	}
	accountName := "acct-policy"
	if err := awconfig.UpdateGlobalAt(os.Getenv("AW_CONFIG_PATH"), func(cfg *awconfig.GlobalConfig) error {
		if cfg.Servers == nil {
			cfg.Servers = map[string]awconfig.Server{}
		}
		if cfg.Accounts == nil {
			cfg.Accounts = map[string]awconfig.Account{}
		}
		cfg.Servers[serverName] = awconfig.Server{URL: server.URL}
		cfg.Accounts[accountName] = awconfig.Account{
			Server:         serverName,
			APIKey:         "aw_sk_from_account",
			DefaultProject: "demo",
			AgentID:        "00000000-0000-0000-0000-000000000000",
			AgentAlias:     "alice",
		}
		cfg.DefaultAccount = accountName
		return nil
	}); err != nil {
		t.Fatalf("seed global config: %v", err)
	}
	if err := awconfig.SaveWorktreeContextTo(filepath.Join(wd, awconfig.DefaultWorktreeContextRelativePath()), &awconfig.WorktreeContext{
		DefaultAccount: accountName,
		ServerAccounts: map[string]string{serverName: accountName},
	}); err != nil {
		t.Fatalf("seed .aw/context: %v", err)
	}

	cfg := &config.Config{BeadhubURL: server.URL}
	result, err := fetchActivePolicyCachedWithConfig(cfg, "coordinator", false, tmp)
	if err != nil {
		t.Fatalf("fetchActivePolicyCachedWithConfig: %v", err)
	}
	if result.Policy == nil || result.Policy.PolicyID != "pol-123" {
		t.Fatalf("unexpected policy: %#v", result.Policy)
	}
}
