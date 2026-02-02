package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndSave(t *testing.T) {
	// Create a temp directory for the test
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Create a config and save it
	cfg := &Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:8000",
		ProjectSlug:     "beadhub",
		RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
		RepoOrigin:      "git@github.com:anthropic/beadhub.git",
		CanonicalOrigin: "github.com/anthropic/beadhub",
		Alias:           "claude-code",
		HumanName:       "Juan",
		Role:            "reviewer",
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(filepath.Join(tmpDir, FileName)); os.IsNotExist(err) {
		t.Fatalf("Config file was not created")
	}

	// Load it back
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Verify all fields
	if loaded.WorkspaceID != cfg.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", loaded.WorkspaceID, cfg.WorkspaceID)
	}
	if loaded.BeadhubURL != cfg.BeadhubURL {
		t.Errorf("BeadhubURL = %q, want %q", loaded.BeadhubURL, cfg.BeadhubURL)
	}
	if loaded.ProjectSlug != cfg.ProjectSlug {
		t.Errorf("ProjectSlug = %q, want %q", loaded.ProjectSlug, cfg.ProjectSlug)
	}
	if loaded.RepoID != cfg.RepoID {
		t.Errorf("RepoID = %q, want %q", loaded.RepoID, cfg.RepoID)
	}
	if loaded.RepoOrigin != cfg.RepoOrigin {
		t.Errorf("RepoOrigin = %q, want %q", loaded.RepoOrigin, cfg.RepoOrigin)
	}
	if loaded.CanonicalOrigin != cfg.CanonicalOrigin {
		t.Errorf("CanonicalOrigin = %q, want %q", loaded.CanonicalOrigin, cfg.CanonicalOrigin)
	}
	if loaded.Alias != cfg.Alias {
		t.Errorf("Alias = %q, want %q", loaded.Alias, cfg.Alias)
	}
	if loaded.HumanName != cfg.HumanName {
		t.Errorf("HumanName = %q, want %q", loaded.HumanName, cfg.HumanName)
	}
	if loaded.Role != cfg.Role {
		t.Errorf("Role = %q, want %q", loaded.Role, cfg.Role)
	}
}

func TestLoadNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	_, err := Load()
	if err == nil {
		t.Error("Load() should return error when file doesn't exist")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Load() error should be IsNotExist, got: %v", err)
	}
}

func TestLoad_FindsConfigInGitRootFromSubdir(t *testing.T) {
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "repo")
	subDir := filepath.Join(repoDir, "nested", "dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git) error: %v", err)
	}

	// Write config at repo root.
	data := []byte(`workspace_id: "a1b2c3d4-5678-90ab-cdef-1234567890ab"
beadhub_url: "http://localhost:8000"
project_slug: "beadhub"
repo_id: "b2c3d4e5-6789-01ab-cdef-234567890abc"
repo_origin: "git@github.com:anthropic/beadhub.git"
canonical_origin: "github.com/anthropic/beadhub"
alias: "claude-code"
human_name: "Juan"
`)
	if err := os.WriteFile(filepath.Join(repoDir, FileName), data, 0600); err != nil {
		t.Fatalf("WriteFile(.beadhub) error: %v", err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.ProjectSlug != "beadhub" {
		t.Errorf("ProjectSlug = %q, want %q", loaded.ProjectSlug, "beadhub")
	}
	if loaded.Alias != "claude-code" {
		t.Errorf("Alias = %q, want %q", loaded.Alias, "claude-code")
	}
}

func TestLoad_DoesNotCrossNestedGitRoots(t *testing.T) {
	tmpDir := t.TempDir()

	outer := filepath.Join(tmpDir, "outer")
	inner := filepath.Join(outer, "inner")
	innerSub := filepath.Join(inner, "subdir")
	if err := os.MkdirAll(innerSub, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outer, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(outer .git) error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(inner .git) error: %v", err)
	}

	// Put config only in the outer repo.
	if err := os.WriteFile(filepath.Join(outer, FileName), []byte("workspace_id: \"a1b2c3d4-5678-90ab-cdef-1234567890ab\"\n"), 0600); err != nil {
		t.Fatalf("WriteFile(outer .beadhub) error: %v", err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if err := os.Chdir(innerSub); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() should error when config is only in outer repo")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("Load() error should be IsNotExist, got: %v", err)
	}
}

func TestSetPath_UsesCustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Reset path after test
	defer SetPath("")

	// Create config in a custom location
	customPath := filepath.Join(tmpDir, "custom", ".beadhub-dev")
	os.MkdirAll(filepath.Dir(customPath), 0755)

	cfg := &Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:9999",
		ProjectSlug:     "test-project",
		RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "dev-agent",
		HumanName:       "Developer",
	}

	// Save to custom path manually
	data := []byte(`workspace_id: "a1b2c3d4-5678-90ab-cdef-1234567890ab"
beadhub_url: "http://localhost:9999"
project_slug: "test-project"
repo_id: "b2c3d4e5-6789-01ab-cdef-234567890abc"
repo_origin: "git@github.com:test/repo.git"
canonical_origin: "github.com/test/repo"
alias: "dev-agent"
human_name: "Developer"
role: "dev"
`)
	if err := os.WriteFile(customPath, data, 0600); err != nil {
		t.Fatalf("Failed to write custom config: %v", err)
	}

	// Set custom path
	SetPath(customPath)

	// Load should use custom path
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.BeadhubURL != cfg.BeadhubURL {
		t.Errorf("BeadhubURL = %q, want %q", loaded.BeadhubURL, cfg.BeadhubURL)
	}
	if loaded.Alias != cfg.Alias {
		t.Errorf("Alias = %q, want %q", loaded.Alias, cfg.Alias)
	}
}

func TestSetPath_ResetToDefault(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Reset path after test
	defer SetPath("")

	// Create default .beadhub
	cfg := &Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:8000",
		ProjectSlug:     "default-project",
		RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "default-agent",
		HumanName:       "Default User",
	}
	cfg.Save()

	// Set and then clear custom path
	SetPath("/some/other/path")
	SetPath("")

	// Should load from default path
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.ProjectSlug != "default-project" {
		t.Errorf("ProjectSlug = %q, want %q", loaded.ProjectSlug, "default-project")
	}
}

func TestGetPath_ReturnsCurrentPath(t *testing.T) {
	// Reset path after test
	defer SetPath("")

	// Default should be FileName
	if GetPath() != FileName {
		t.Errorf("GetPath() = %q, want %q", GetPath(), FileName)
	}

	// After setting custom path
	SetPath("/custom/path/.beadhub")
	if GetPath() != "/custom/path/.beadhub" {
		t.Errorf("GetPath() = %q, want %q", GetPath(), "/custom/path/.beadhub")
	}

	// After resetting
	SetPath("")
	if GetPath() != FileName {
		t.Errorf("GetPath() = %q, want %q", GetPath(), FileName)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config with ssh repo origin",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "http://localhost:8000",
				ProjectSlug:     "beadhub",
				RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
				RepoOrigin:      "git@github.com:anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "claude-code",
				HumanName:       "Juan",
				Role:            "reviewer",
			},
			wantErr: false,
		},
		{
			name: "valid config with https repo origin",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "https://beadhub.cloud",
				ProjectSlug:     "my-project",
				RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
				RepoOrigin:      "https://github.com/anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "backend_bot",
				HumanName:       "Maria O'Brien",
			},
			wantErr: false,
		},
		{
			name: "valid config with two-word role",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "http://localhost:8000",
				ProjectSlug:     "beadhub",
				RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
				RepoOrigin:      "git@github.com:anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "claude-code",
				HumanName:       "Juan",
				Role:            "full stack",
			},
			wantErr: false,
		},
		{
			name: "role too many words",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "http://localhost:8000",
				ProjectSlug:     "beadhub",
				RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
				RepoOrigin:      "git@github.com:anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "claude-code",
				HumanName:       "Juan",
				Role:            "full stack programmer",
			},
			wantErr: true,
		},
		{
			name: "role invalid characters",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "http://localhost:8000",
				ProjectSlug:     "beadhub",
				RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
				RepoOrigin:      "git@github.com:anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "claude-code",
				HumanName:       "Juan",
				Role:            "full$stack",
			},
			wantErr: true,
		},
		{
			name: "role too long",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "http://localhost:8000",
				ProjectSlug:     "beadhub",
				RepoID:          "b2c3d4e5-6789-01ab-cdef-234567890abc",
				RepoOrigin:      "git@github.com:anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "claude-code",
				HumanName:       "Juan",
				Role:            strings.Repeat("a", 51),
			},
			wantErr: true,
		},
		{
			name: "missing workspace_id",
			cfg: Config{
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude-code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid url",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "not-a-url",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude-code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid alias with spaces",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid alias starting with underscore",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "_claude",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid alias starting with dash",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "-claude",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid alias too long",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "a" + strings.Repeat("b", 64),
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid project_slug with uppercase",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "BeadHub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude-code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid human_name starting with number",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude-code",
				HumanName:   "123Juan",
			},
			wantErr: true,
		},
		{
			name: "valid human_name with digits after first letter",
			cfg: Config{
				WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:      "http://localhost:8000",
				ProjectSlug:     "beadhub",
				RepoOrigin:      "git@github.com:anthropic/beadhub.git",
				CanonicalOrigin: "github.com/anthropic/beadhub",
				Alias:           "claude-code",
				HumanName:       "Juan2",
			},
			wantErr: false,
		},
		{
			name: "invalid workspace_id not a uuid",
			cfg: Config{
				WorkspaceID: "not-a-uuid",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude-code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "invalid uuid with uppercase",
			cfg: Config{
				WorkspaceID: "A1B2C3D4-5678-90AB-CDEF-1234567890AB",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				Alias:       "claude-code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "missing repo_origin",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				Alias:       "claude-code",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
		{
			name: "missing alias",
			cfg: Config{
				WorkspaceID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				BeadhubURL:  "http://localhost:8000",
				ProjectSlug: "beadhub",
				RepoOrigin:  "git@github.com:anthropic/beadhub.git",
				HumanName:   "Juan",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitizeSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Basic cases
		{"my-project", "my-project"},
		{"myproject", "myproject"},
		{"my-project-123", "my-project-123"},

		// Underscores become hyphens
		{"my_project", "my-project"},
		{"my__project", "my-project"},

		// Dots become hyphens
		{"my.project", "my-project"},
		{"my..project", "my-project"},

		// Spaces become hyphens
		{"my project", "my-project"},
		{"my  project", "my-project"},

		// Uppercase becomes lowercase
		{"My-Project", "my-project"},
		{"MY_PROJECT", "my-project"},

		// Multiple hyphens collapsed
		{"my--project", "my-project"},
		{"my---project", "my-project"},

		// Leading/trailing hyphens removed
		{"-my-project", "my-project"},
		{"my-project-", "my-project"},
		{"-my-project-", "my-project"},

		// Special chars removed
		{"my@project", "myproject"},
		{"my#project", "myproject"},
		{"my$project", "myproject"},
		{"my!project", "myproject"},

		// Digits at start (allowed)
		{"123project", "123project"},
		{"123-project", "123-project"},
		{"123_test", "123-test"},

		// Mixed cases
		{"My_Project.Name", "my-project-name"},
		{"  my project  ", "my-project"},

		// Edge cases
		{"", ""},
		{"-", ""},
		{"--", ""},
		{"___", ""},
		{"@#$", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeSlug(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidSlug(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"my-project", true},
		{"myproject", true},
		{"my-project-123", true},
		{"123-project", true},
		{"a", true},
		{"my_project", false},  // underscore not allowed
		{"My-Project", false},  // uppercase not allowed
		{"-my-project", false}, // leading hyphen not allowed
		{"my-project-", true},  // trailing hyphen allowed by pattern
		{"", false},
		{strings.Repeat("a", 64), false}, // too long
		{strings.Repeat("a", 63), true},  // max length ok
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidSlug(tt.input)
			if got != tt.valid {
				t.Errorf("IsValidSlug(%q) = %v, want %v", tt.input, got, tt.valid)
			}
		})
	}
}
