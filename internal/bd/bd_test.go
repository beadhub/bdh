package bd

import (
	"context"
	"testing"
)

func TestRun_Echo(t *testing.T) {
	r := &Runner{BdPath: "echo"}
	result, err := r.Run(context.Background(), []string{"hello", "world"})

	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("Stdout = %q, want %q", result.Stdout, "hello world\n")
	}
}

// TestRun_ArgsWithSpaces verifies that arguments containing spaces are passed
// correctly as single arguments.
func TestRun_ArgsWithSpaces(t *testing.T) {
	// Use printf to show each arg on a separate line, proving they're separate args
	r := &Runner{BdPath: "printf"}
	result, err := r.Run(context.Background(), []string{"%s\\n", "arg1", "arg with spaces", "arg3"})

	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}

	// printf with %s\n format prints each arg on a new line
	// The first arg is the format string, so we get 3 lines for the 3 remaining args
	expected := "arg1\narg with spaces\narg3\n"
	if result.Stdout != expected {
		t.Errorf("Stdout = %q, want %q\nArgs with spaces should be preserved as single arguments", result.Stdout, expected)
	}
}

func TestRun_ExitCode(t *testing.T) {
	r := &Runner{BdPath: "sh"}
	result, err := r.Run(context.Background(), []string{"-c", "exit 42"})

	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42", result.ExitCode)
	}
}

func TestRun_Stderr(t *testing.T) {
	r := &Runner{BdPath: "sh"}
	result, err := r.Run(context.Background(), []string{"-c", "echo error >&2"})

	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result.Stderr != "error\n" {
		t.Errorf("Stderr = %q, want %q", result.Stderr, "error\n")
	}
}

func TestRun_NotFound(t *testing.T) {
	r := &Runner{BdPath: "/nonexistent/command"}
	_, err := r.Run(context.Background(), []string{})

	if err == nil {
		t.Error("Expected error for nonexistent command")
	}
}

func TestRun_ContextCanceled(t *testing.T) {
	r := &Runner{BdPath: "sleep"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := r.Run(ctx, []string{"10"})

	if err == nil {
		t.Error("Expected error for canceled context")
	}
}

func TestIsMutationCommand(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"create", "--title", "Test"}, true},
		{[]string{"--db", ".beads/beads.db", "create", "--title", "Test"}, true},
		{[]string{"--db=.beads/beads.db", "create", "--title", "Test"}, true},
		{[]string{"close", "bd-42"}, true},
		{[]string{"update", "bd-42", "--status", "in_progress"}, true},
		{[]string{"delete", "bd-42"}, true},
		{[]string{"reopen", "bd-42"}, true},
		{[]string{"sync"}, true},
		// dep command - all subcommands trigger sync
		{[]string{"dep", "add", "bd-43", "bd-42"}, true},
		{[]string{"dep", "remove", "bd-43", "bd-42"}, true},
		{[]string{"dep", "relate", "bd-43", "bd-42"}, true},
		{[]string{"dep", "unrelate", "bd-43", "bd-42"}, true},
		{[]string{"dep", "bd-42", "--blocks", "bd-43"}, true},
		{[]string{"--db", ".beads/beads.db", "dep", "add", "bd-43", "bd-42"}, true},
		// read-only dep subcommands also trigger sync (conservative approach)
		{[]string{"dep", "list", "bd-42"}, true},
		{[]string{"dep", "tree", "bd-42"}, true},
		{[]string{"dep", "cycles"}, true},
		{[]string{"list"}, false},
		{[]string{"show", "bd-42"}, false},
		{[]string{"ready"}, false},
		{[]string{}, false},
		// Edge cases: flags at end without values (should not panic)
		{[]string{"--db"}, false},
		{[]string{"--actor"}, false},
		{[]string{"--lock-timeout"}, false},
		// Edge case: flag followed by another flag (value is the next flag)
		{[]string{"--db", "--no-daemon", "create"}, true},
	}

	for _, tt := range tests {
		got := IsMutationCommand(tt.args)
		if got != tt.want {
			t.Errorf("IsMutationCommand(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestNew(t *testing.T) {
	r := New()
	if r.BdPath != "bd" {
		t.Errorf("BdPath = %q, want %q", r.BdPath, "bd")
	}
}
