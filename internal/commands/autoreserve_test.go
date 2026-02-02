package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/config"
)

func TestParseGitStatusPorcelainV1Z_ParsesRenameEntries(t *testing.T) {
	out := []byte(" M file1.txt\x00?? new.txt\x00R  old.txt\x00new.txt\x00D  gone.txt\x00")

	entries, err := parseGitStatusPorcelainV1Z(out)
	if err != nil {
		t.Fatalf("parseGitStatusPorcelainV1Z error: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	if entries[0].X != ' ' || entries[0].Y != 'M' || entries[0].Path != "file1.txt" {
		t.Fatalf("unexpected entry[0]: %+v", entries[0])
	}
	if entries[1].X != '?' || entries[1].Y != '?' || entries[1].Path != "new.txt" {
		t.Fatalf("unexpected entry[1]: %+v", entries[1])
	}
	if entries[2].X != 'R' || entries[2].Path != "new.txt" || entries[2].OrigPath != "old.txt" {
		t.Fatalf("unexpected rename entry: %+v", entries[2])
	}
	if entries[3].X != 'D' || entries[3].Path != "gone.txt" {
		t.Fatalf("unexpected entry[3]: %+v", entries[3])
	}
}

func TestDesiredLockPaths_ExcludesDeletedAndUntrackedByDefault(t *testing.T) {
	entries := []gitStatusEntry{
		{X: ' ', Y: 'M', Path: "modified.txt"},
		{X: '?', Y: '?', Path: "untracked.txt"},
		{X: 'D', Y: ' ', Path: "deleted.txt"},
		{X: ' ', Y: 'M', Path: "../traversal.txt"},
	}

	desired := desiredLockPaths(entries, false)
	if _, ok := desired["modified.txt"]; !ok {
		t.Fatalf("expected modified.txt to be reserved")
	}
	if _, ok := desired["untracked.txt"]; ok {
		t.Fatalf("did not expect untracked.txt to be reserved by default")
	}
	if _, ok := desired["deleted.txt"]; ok {
		t.Fatalf("did not expect deleted.txt to be reserved")
	}
	if _, ok := desired["../traversal.txt"]; ok {
		t.Fatalf("did not expect traversal path to be reserved")
	}

	desiredWithUntracked := desiredLockPaths(entries, true)
	if _, ok := desiredWithUntracked["untracked.txt"]; !ok {
		t.Fatalf("expected untracked.txt to be reserved when enabled")
	}
}

func TestConfig_SaveIncludesAutoReserveFields(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	autoReserve := false
	reserveUntracked := true
	cfg := &config.Config{
		WorkspaceID:      "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:       "http://localhost:8000",
		ProjectSlug:      "test-project",
		RepoID:           "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:       "git@github.com:test/repo.git",
		CanonicalOrigin:  "github.com/test/repo",
		Alias:            "test-agent",
		HumanName:        "Test Human",
		AutoReserve:      &autoReserve,
		ReserveUntracked: &reserveUntracked,
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, config.FileName))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "auto_reserve: false") {
		t.Fatalf("expected auto_reserve to be saved, got:\n%s", text)
	}
	if !strings.Contains(text, "reserve_untracked: true") {
		t.Fatalf("expected reserve_untracked to be saved, got:\n%s", text)
	}
}

func TestAutoReserve_RenewsExistingAutoLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses git and assumes unix-like paths")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")

	filePath := filepath.Join(repoDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("v1\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit("add", "file.txt")
	runGit("commit", "-m", "init")

	if err := os.WriteFile(filePath, []byte("v2\n"), 0644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	var acquireCalls int
	var renewCalls int
	var releaseCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/reservations":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"reservations": []map[string]any{
						{
							"project_id":      "b2c3d4e5-6789-01bc-def0-234567890abc",
							"resource_key":    "file.txt",
							"holder_agent_id": "a1b2c3d4-5678-90ab-cdef-1234567890ab",
							"holder_alias":    "test-agent",
							"acquired_at":     "2025-01-01T00:00:00Z",
							"expires_at":      "2025-01-01T00:05:00Z",
							"metadata": map[string]any{
								"reason": "auto-reserve",
							},
						},
					},
				})
				return
			}
			if r.Method == http.MethodPost {
				acquireCalls++
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		case "/v1/reservations/renew":
			renewCalls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["resource_key"] != "file.txt" {
				t.Fatalf("renew resource_key=%v, want file.txt", body["resource_key"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       "renewed",
				"resource_key": "file.txt",
				"expires_at":   "2025-01-01T00:05:00Z",
			})
			return
		case "/v1/reservations/release":
			releaseCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       "released",
				"resource_key": "file.txt",
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}

	aw, err := aweb.NewWithAPIKey(server.URL, "test-api-key")
	if err != nil {
		t.Fatalf("aweb.NewWithAPIKey: %v", err)
	}

	res := autoReserve(context.Background(), cfg, aw)
	if res == nil {
		t.Fatalf("expected autoReserve to take action (renew), got nil")
	}
	if renewCalls != 1 {
		t.Fatalf("expected 1 renew call, got %d", renewCalls)
	}
	if acquireCalls != 0 {
		t.Fatalf("expected 0 acquire calls, got %d", acquireCalls)
	}
	if releaseCalls != 0 {
		t.Fatalf("expected 0 release calls, got %d", releaseCalls)
	}
	if len(res.Renewed) != 1 || res.Renewed[0] != "file.txt" {
		t.Fatalf("renewed=%v, want [file.txt]", res.Renewed)
	}
	if len(res.Acquired) != 0 {
		t.Fatalf("acquired=%v, want []", res.Acquired)
	}
	if len(res.Released) != 0 {
		t.Fatalf("released=%v, want []", res.Released)
	}
}

func TestAutoReserve_ReleasesStaleAutoLocks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses git and assumes unix-like paths")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@example.com")
	runGit("config", "user.name", "Test")

	filePath := filepath.Join(repoDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("v1\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit("add", "file.txt")
	runGit("commit", "-m", "init")

	var gotReleaseKeys []string
	var acquireCalls int
	var renewCalls int
	var releaseCalls int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/reservations":
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"reservations": []map[string]any{
						{
							"project_id":      "b2c3d4e5-6789-01bc-def0-234567890abc",
							"resource_key":    "file.txt",
							"holder_agent_id": "a1b2c3d4-5678-90ab-cdef-1234567890ab",
							"holder_alias":    "test-agent",
							"acquired_at":     "2025-01-01T00:00:00Z",
							"expires_at":      "2025-01-01T00:05:00Z",
							"metadata": map[string]any{
								"reason": "auto-reserve",
							},
						},
					},
				})
				return
			}
			if r.Method == http.MethodPost {
				acquireCalls++
				w.WriteHeader(http.StatusNotFound)
				return
			}
		case "/v1/reservations/renew":
			renewCalls++
			w.WriteHeader(http.StatusNotFound)
			return
		case "/v1/reservations/release":
			releaseCalls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode release request: %v", err)
			}
			if rk, ok := body["resource_key"].(string); ok {
				gotReleaseKeys = append(gotReleaseKeys, rk)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       "released",
				"resource_key": "file.txt",
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}

	aw, err := aweb.NewWithAPIKey(server.URL, "test-api-key")
	if err != nil {
		t.Fatalf("aweb.NewWithAPIKey: %v", err)
	}

	res := autoReserve(context.Background(), cfg, aw)
	if res == nil {
		t.Fatalf("expected autoReserve to take action (release), got nil")
	}
	if acquireCalls != 0 {
		t.Fatalf("expected 0 acquire calls, got %d", acquireCalls)
	}
	if renewCalls != 0 {
		t.Fatalf("expected 0 renew calls, got %d", renewCalls)
	}
	if releaseCalls != 1 {
		t.Fatalf("expected 1 release call, got %d", releaseCalls)
	}
	if len(gotReleaseKeys) != 1 || gotReleaseKeys[0] != "file.txt" {
		t.Fatalf("released keys=%v, want [file.txt]", gotReleaseKeys)
	}
	if len(res.Released) != 1 || res.Released[0] != "file.txt" {
		t.Fatalf("released=%v, want [file.txt]", res.Released)
	}
}

func TestValidateGitRepoPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid absolute unix path",
			path:    "/home/user/project",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "relative path",
			path:    "project/src",
			wantErr: true,
		},
		{
			name:    "path with dot-dot",
			path:    "/home/user/../project",
			wantErr: true,
		},
		{
			name:    "path with trailing slash",
			path:    "/home/user/project/",
			wantErr: true,
		},
		{
			name:    "path with double slash",
			path:    "/home/user//project",
			wantErr: true,
		},
	}

	// Add platform-specific test for Windows
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name    string
			path    string
			wantErr bool
		}{
			name:    "valid absolute windows path",
			path:    `C:\Users\project`,
			wantErr: false,
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitRepoPath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateGitRepoPath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
