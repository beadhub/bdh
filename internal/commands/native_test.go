package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/config"
)

func TestParseNativeCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantCmd string
		wantErr bool
	}{
		{
			name:    "create command",
			args:    []string{"create", "--title", "Test"},
			wantCmd: "create",
		},
		{
			name:    "list command",
			args:    []string{"list", "--status=open"},
			wantCmd: "list",
		},
		{
			name:    "show command",
			args:    []string{"show", "bdh-42"},
			wantCmd: "show",
		},
		{
			name:    "update command",
			args:    []string{"update", "bdh-42", "--status", "in_progress"},
			wantCmd: "update",
		},
		{
			name:    "close command",
			args:    []string{"close", "bdh-42"},
			wantCmd: "close",
		},
		{
			name:    "ready command",
			args:    []string{"ready"},
			wantCmd: "ready",
		},
		{
			name:    "blocked command",
			args:    []string{"blocked"},
			wantCmd: "blocked",
		},
		{
			name:    "dep command",
			args:    []string{"dep", "add", "bdh-42", "bdh-43"},
			wantCmd: "dep",
		},
		{
			name:    "sync command",
			args:    []string{"sync"},
			wantCmd: "sync",
		},
		{
			name:    "stats command",
			args:    []string{"stats"},
			wantCmd: "stats",
		},
		{
			name:    "reopen command",
			args:    []string{"reopen", "bdh-42"},
			wantCmd: "reopen",
		},
		{
			name:    "unsupported command",
			args:    []string{"search", "foo"},
			wantErr: true,
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "help flag passthrough",
			args:    []string{"--help"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _, err := parseNativeCommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseNativeCommand(%v) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Errorf("parseNativeCommand(%v) error = %v, want nil", tt.args, err)
				return
			}
			if cmd != tt.wantCmd {
				t.Errorf("parseNativeCommand(%v) cmd = %q, want %q", tt.args, cmd, tt.wantCmd)
			}
		})
	}
}

func TestParseNativeCommand_WithGlobalFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantCmd       string
		wantRemaining []string
	}{
		{
			name:          "db flag before command",
			args:          []string{"--db", ".beads/beads.db", "create", "--title", "Test"},
			wantCmd:       "create",
			wantRemaining: []string{"--title", "Test"},
		},
		{
			name:          "db= flag before command",
			args:          []string{"--db=.beads/beads.db", "list", "--status=open"},
			wantCmd:       "list",
			wantRemaining: []string{"--status=open"},
		},
		{
			name:          "actor flag with value matching command name",
			args:          []string{"--actor", "list", "list", "--status=open"},
			wantCmd:       "list",
			wantRemaining: []string{"--status=open"},
		},
		{
			name:          "no remaining args",
			args:          []string{"ready"},
			wantCmd:       "ready",
			wantRemaining: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, remaining, err := parseNativeCommand(tt.args)
			if err != nil {
				t.Fatalf("parseNativeCommand(%v) error = %v", tt.args, err)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(remaining) != len(tt.wantRemaining) {
				t.Fatalf("remaining = %v (len %d), want %v (len %d)", remaining, len(remaining), tt.wantRemaining, len(tt.wantRemaining))
			}
			for i := range remaining {
				if remaining[i] != tt.wantRemaining[i] {
					t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], tt.wantRemaining[i])
				}
			}
		})
	}
}

func TestNativeCommandsAreListed(t *testing.T) {
	// Verify all supported commands are recognized
	supported := []string{
		"create", "list", "show", "update", "close",
		"ready", "blocked", "dep", "sync", "stats", "reopen",
	}
	for _, cmd := range supported {
		if !isNativeCommand(cmd) {
			t.Errorf("isNativeCommand(%q) = false, want true", cmd)
		}
	}

	// Verify unsupported commands are rejected
	unsupported := []string{"search", "export", "import", "init", "edit", "epic", "swarm"}
	for _, cmd := range unsupported {
		if isNativeCommand(cmd) {
			t.Errorf("isNativeCommand(%q) = true, want false", cmd)
		}
	}
}

func TestPassthrough_UsesNativeModeWhenBeadsNotInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// NO .beads directory — native mode
	beads.ResetCache()

	// Mock server that approves
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bdh/command":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
		case "/v1/chat/pending":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pending":          []any{},
				"messages_waiting": 0,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

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
	cfg.Save()

	_, err := runPassthrough([]string{"list", "--status=open"})

	// Should get a native mode error (not yet implemented)
	if err == nil {
		t.Fatal("expected native mode error, got nil")
	}
	if !strings.Contains(err.Error(), "native mode") {
		t.Errorf("expected 'native mode' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' in error, got: %v", err)
	}
}

func TestPassthrough_ErrorsWhenNoConfigAndNoBeads(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// NO .beadhub config, NO .beads directory
	beads.ResetCache()

	_, err := runPassthrough([]string{"list"})

	if err == nil {
		t.Fatal("expected error when no config and no beads, got nil")
	}
	if !strings.Contains(err.Error(), "bdh :init") {
		t.Errorf("expected error to suggest 'bdh :init', got: %v", err)
	}
}

func TestPassthrough_UsesNativeModeForUnsupportedCommand(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// NO .beads directory — native mode
	beads.ResetCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bdh/command":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
		case "/v1/chat/pending":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pending":          []any{},
				"messages_waiting": 0,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

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
	cfg.Save()

	_, err := runPassthrough([]string{"search", "foo"})

	// Should get a native mode error about unsupported command
	if err == nil {
		t.Fatal("expected native mode error for unsupported command, got nil")
	}
	if !strings.Contains(err.Error(), "not available in native mode") {
		t.Errorf("expected 'not available in native mode' in error, got: %v", err)
	}
}
