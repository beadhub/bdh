package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasBdhInstructions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "no bdh markers",
			content: "# My Project\n\nSome documentation here.",
			want:    false,
		},
		{
			name:    "has injected section header",
			content: "# Project\n\n## BeadHub Coordination\n\nSome content.",
			want:    true,
		},
		{
			name:    "BeadHub mention without section header",
			content: "This project uses BeadHub for coordination.",
			want:    false,
		},
		{
			name:    "bd instructions only",
			content: "Run `bd ready` to find work.",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasBdhInstructions(tt.content)
			if got != tt.want {
				t.Errorf("hasBdhInstructions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBdMarkerRegex(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no bd markers",
			content: "# My Project",
			want:    false,
		},
		{
			name:    "bd ready",
			content: "Run `bd ready` to find work.",
			want:    true,
		},
		{
			name:    "bd create",
			content: "Use bd create to make issues.",
			want:    true,
		},
		{
			name:    "bd prime",
			content: "Run bd prime for context.",
			want:    true,
		},
		{
			name:    "bd sync",
			content: "Always run bd sync at session end.",
			want:    true,
		},
		{
			name:    "bd close",
			content: "Use bd close to complete work.",
			want:    true,
		},
		{
			name:    "bd update",
			content: "Run bd update to claim work.",
			want:    true,
		},
		{
			name:    "bdh ready should not match",
			content: "Run bdh ready to find work.",
			want:    false,
		},
		{
			name:    "embedded in word should not match",
			content: "The bday party was fun.",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bdMarkerRegex.MatchString(tt.content)
			if got != tt.want {
				t.Errorf("bdMarkerRegex.MatchString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInjectAgentDocs_NoFiles(t *testing.T) {
	// Create temp directory with no CLAUDE.md or AGENTS.md
	tmpDir := t.TempDir()

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Injected) != 0 {
		t.Errorf("Expected no injections, got %v", result.Injected)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Expected no skips, got %v", result.Skipped)
	}
	if len(result.Upgraded) != 0 {
		t.Errorf("Expected no upgrades, got %v", result.Upgraded)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Expected no errors, got %v", result.Errors)
	}
}

func TestInjectAgentDocs_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create empty CLAUDE.md
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Injected) != 1 || result.Injected[0] != "CLAUDE.md" {
		t.Errorf("Expected CLAUDE.md to be injected, got %v", result.Injected)
	}

	// Verify content was written
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "bdh :status") {
		t.Error("Expected injected content to contain 'bdh :status'")
	}
	if !strings.Contains(string(content), "BeadHub Coordination") {
		t.Error("Expected injected content to contain 'BeadHub Coordination'")
	}
}

func TestInjectAgentDocs_WithBdInstructions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create AGENTS.md with bd instructions
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	originalContent := "# Agent Instructions\n\nRun `bd ready` to find work.\n"
	if err := os.WriteFile(agentsPath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Upgraded) != 1 || result.Upgraded[0] != "AGENTS.md" {
		t.Errorf("Expected AGENTS.md to be upgraded, got %v", result.Upgraded)
	}

	// Verify content contains upgrade notice
	content, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "IMPORTANT") {
		t.Error("Expected upgrade notice with 'IMPORTANT'")
	}
	if !strings.Contains(contentStr, "bdh :status") {
		t.Error("Expected injected content to contain 'bdh :status'")
	}
	// Original content should be preserved
	if !strings.Contains(contentStr, "bd ready") {
		t.Error("Expected original content to be preserved")
	}
}

func TestInjectAgentDocs_AlreadyHasBdh(t *testing.T) {
	tmpDir := t.TempDir()

	// Create CLAUDE.md with bdh instructions already (has injected section header)
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	originalContent := "# My Project\n\n## BeadHub Coordination\n\nAlready has instructions.\n"
	if err := os.WriteFile(claudePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Skipped) != 1 || result.Skipped[0] != "CLAUDE.md" {
		t.Errorf("Expected CLAUDE.md to be skipped, got skipped=%v", result.Skipped)
	}
	if len(result.Injected) != 0 {
		t.Errorf("Expected no injections, got %v", result.Injected)
	}

	// Verify content was NOT modified
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(content) != originalContent {
		t.Error("Content should not have been modified")
	}
}

func TestInjectAgentDocs_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create empty CLAUDE.md
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("# Project\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// First injection
	result1, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("First InjectAgentDocs() error = %v", err)
	}
	if len(result1.Injected) != 1 {
		t.Errorf("First injection: expected 1 injection, got %v", result1.Injected)
	}

	// Read content after first injection
	content1, _ := os.ReadFile(claudePath)

	// Second injection (should skip)
	result2, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("Second InjectAgentDocs() error = %v", err)
	}
	if len(result2.Skipped) != 1 {
		t.Errorf("Second injection: expected 1 skip, got skipped=%v", result2.Skipped)
	}
	if len(result2.Injected) != 0 {
		t.Errorf("Second injection: expected 0 injections, got %v", result2.Injected)
	}

	// Content should be unchanged
	content2, _ := os.ReadFile(claudePath)
	if string(content1) != string(content2) {
		t.Error("Content should not change on second injection")
	}
}

func TestInjectAgentDocs_Symlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create actual file in a subdirectory
	docsDir := filepath.Join(tmpDir, "docs")
	if err := os.Mkdir(docsDir, 0755); err != nil {
		t.Fatalf("Failed to create docs dir: %v", err)
	}
	actualPath := filepath.Join(docsDir, "AGENTS-template.md")
	if err := os.WriteFile(actualPath, []byte("# Template\n"), 0644); err != nil {
		t.Fatalf("Failed to create template file: %v", err)
	}

	// Create symlinks for both CLAUDE.md and AGENTS.md pointing to same file
	claudeLink := filepath.Join(tmpDir, "CLAUDE.md")
	agentsLink := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.Symlink(actualPath, claudeLink); err != nil {
		t.Fatalf("Failed to create CLAUDE.md symlink: %v", err)
	}
	if err := os.Symlink(actualPath, agentsLink); err != nil {
		t.Fatalf("Failed to create AGENTS.md symlink: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	// Should only inject once (first file wins, second is skipped because same resolved path)
	totalModified := len(result.Injected) + len(result.Upgraded)
	if totalModified != 1 {
		t.Errorf("Expected exactly 1 modification for symlinked files, got injected=%v, upgraded=%v",
			result.Injected, result.Upgraded)
	}

	// Verify content was written to the actual file
	content, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("Failed to read actual file: %v", err)
	}
	if !strings.Contains(string(content), "bdh :status") {
		t.Error("Expected injected content in actual file")
	}
}

func TestInjectAgentDocs_BothFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create both files (not symlinked)
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	if err := os.WriteFile(claudePath, []byte("# Claude\n"), 0644); err != nil {
		t.Fatalf("Failed to create CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(agentsPath, []byte("# Agents\n"), 0644); err != nil {
		t.Fatalf("Failed to create AGENTS.md: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Injected) != 2 {
		t.Errorf("Expected 2 injections, got %v", result.Injected)
	}

	// Verify both files have content
	for _, path := range []string{claudePath, agentsPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read %s: %v", path, err)
		}
		if !strings.Contains(string(content), "bdh :status") {
			t.Errorf("Expected bdh instructions in %s", path)
		}
	}
}

func TestInjectAgentDocs_WriteFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create read-only file
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("# Project\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.Chmod(claudePath, 0444); err != nil {
		t.Fatalf("Failed to make file read-only: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error for write failure, got %v", result.Errors)
	}
	if len(result.Injected) != 0 {
		t.Errorf("Expected no injections on write failure, got %v", result.Injected)
	}
}

func TestInjectAgentDocs_BrokenSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create symlink to non-existent file
	claudeLink := filepath.Join(tmpDir, "CLAUDE.md")
	nonExistent := filepath.Join(tmpDir, "does-not-exist.md")
	if err := os.Symlink(nonExistent, claudeLink); err != nil {
		t.Fatalf("Failed to create broken symlink: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	// Should record an error for the broken symlink
	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error for broken symlink, got %v", result.Errors)
	}
}

func TestInjectAgentDocs_PreservesPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with specific permissions
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("# Project\n"), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result, err := InjectAgentDocs(tmpDir)
	if err != nil {
		t.Fatalf("InjectAgentDocs() error = %v", err)
	}

	if len(result.Injected) != 1 {
		t.Errorf("Expected 1 injection, got %v", result.Injected)
	}

	// Verify permissions are preserved
	info, err := os.Stat(claudePath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Expected permissions 0600, got %o", info.Mode().Perm())
	}
}

func TestBdMarkerRegex_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "lowercase bd ready",
			content: "Run bd ready to find work.",
			want:    true,
		},
		{
			name:    "uppercase BD READY",
			content: "Run BD READY to find work.",
			want:    true,
		},
		{
			name:    "mixed case Bd Ready",
			content: "Run Bd Ready to find work.",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bdMarkerRegex.MatchString(tt.content)
			if got != tt.want {
				t.Errorf("bdMarkerRegex.MatchString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBdToBdhReplacements(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "backtick bd command",
			input: "Run `bd ready` to find work.",
			want:  "Run `bdh ready` to find work.",
		},
		{
			name:  "bare bd command",
			input: "bd ready",
			want:  "bdh ready",
		},
		{
			name:  "bd at line start",
			input: "bd close bd-123",
			want:  "bdh close bd-123",
		},
		{
			name:  "multiple bd commands",
			input: "Run `bd ready` then `bd close`",
			want:  "Run `bdh ready` then `bdh close`",
		},
		{
			name:  "bd sync command",
			input: "Always run bd sync at session end.",
			want:  "Always run bdh sync at session end.",
		},
		{
			name:  "bd create command",
			input: "Use bd create to make issues.",
			want:  "Use bdh create to make issues.",
		},
		{
			name:  "Run bd pattern",
			input: "Run `bd sync` to sync.",
			want:  "Run `bdh sync` to sync.",
		},
		{
			name:  "bare backtick bd",
			input: "Use `bd` for tracking.",
			want:  "Use `bdh` for tracking.",
		},
		{
			name:  "should not replace bd in bead ID",
			input: "Close bd-123 when done.",
			want:  "Close bd-123 when done.",
		},
		{
			name:  "prefer bd with em-dash",
			input: "prefer bd—persistence",
			want:  "prefer bdh—persistence",
		},
		{
			name:  "prefer bd with comma",
			input: "prefer bd, it's better",
			want:  "prefer bdh, it's better",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.input
			for _, r := range bdToBdhReplacements {
				result = r.pattern.ReplaceAllString(result, r.replacement)
			}
			if result != tt.want {
				t.Errorf("replacement result = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestInjectPrimeOverride_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()

	result := InjectPrimeOverride(tmpDir)

	if result.Injected {
		t.Error("Expected no injection when .beads dir doesn't exist")
	}
	if result.Skipped {
		t.Error("Expected no skip when .beads dir doesn't exist")
	}
	if result.Error != "" {
		t.Errorf("Expected no error, got %s", result.Error)
	}
}

func TestInjectPrimeOverride_AlreadyHasBdhContent(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create PRIME.md with bdh content already
	primePath := filepath.Join(beadsDir, "PRIME.md")
	existingContent := "# Beads Workflow\n\nUse bdh for coordination.\n"
	if err := os.WriteFile(primePath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write PRIME.md: %v", err)
	}

	result := InjectPrimeOverride(tmpDir)

	if result.Injected {
		t.Error("Expected no injection when bdh content already present")
	}
	if !result.Skipped {
		t.Error("Expected skip when bdh content already present")
	}

	// Verify content unchanged
	content, _ := os.ReadFile(primePath)
	if string(content) != existingContent {
		t.Error("Content should not have changed")
	}
}

func TestInjectPrimeOverride_AlreadyHasBeadHubMarker(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}

	// Create PRIME.md with BeadHub marker
	primePath := filepath.Join(beadsDir, "PRIME.md")
	existingContent := "# Beads Workflow Context (BeadHub)\n\nContent here.\n"
	if err := os.WriteFile(primePath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write PRIME.md: %v", err)
	}

	result := InjectPrimeOverride(tmpDir)

	if result.Injected {
		t.Error("Expected no injection when BeadHub marker already present")
	}
	if !result.Skipped {
		t.Error("Expected skip when BeadHub marker already present")
	}
}

func TestInjectPrimeOverride_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}
	primePath := filepath.Join(beadsDir, "PRIME.md")

	// First call - should inject (requires bd to be available)
	result1 := InjectPrimeOverride(tmpDir)

	// If bd is not available, the test should skip gracefully
	if result1.Error != "" && strings.Contains(result1.Error, "bd prime") {
		t.Skip("bd command not available, skipping integration test")
	}

	if !result1.Injected {
		t.Error("First call: expected injection")
	}

	// Read content after first injection
	content1, _ := os.ReadFile(primePath)

	// Second call - should skip
	result2 := InjectPrimeOverride(tmpDir)

	if result2.Injected {
		t.Error("Second call: expected no injection")
	}
	if !result2.Skipped {
		t.Error("Second call: expected skip")
	}

	// Content should be unchanged
	content2, _ := os.ReadFile(primePath)
	if string(content1) != string(content2) {
		t.Error("Content should not change on second call")
	}
}

func TestInjectPrimeOverride_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatalf("Failed to create .beads dir: %v", err)
	}
	primePath := filepath.Join(beadsDir, "PRIME.md")

	result := InjectPrimeOverride(tmpDir)

	// If bd is not available, skip gracefully
	if result.Error != "" && strings.Contains(result.Error, "bd prime") {
		t.Skip("bd command not available, skipping integration test")
	}

	if !result.Injected {
		t.Errorf("Expected injection, got error: %s", result.Error)
	}

	// Verify content has bdh references
	content, err := os.ReadFile(primePath)
	if err != nil {
		t.Fatalf("Failed to read PRIME.md: %v", err)
	}

	contentStr := string(content)

	// Check header is present
	if !strings.Contains(contentStr, "BeadHub Workspace") {
		t.Error("Expected BeadHub Workspace header in PRIME.md")
	}

	// Check session commands are present
	if !strings.Contains(contentStr, "bdh :status") {
		t.Error("Expected bdh :status in PRIME.md")
	}
	if !strings.Contains(contentStr, "bdh :policy") {
		t.Error("Expected bdh :policy in PRIME.md")
	}

	// Check bd→bdh replacement worked (look for bdh ready, not bd ready)
	if !strings.Contains(contentStr, "bdh ready") {
		t.Error("Expected bdh ready (bd should be replaced with bdh)")
	}
}
