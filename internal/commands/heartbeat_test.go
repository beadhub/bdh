package commands

import (
	"os/exec"
	"testing"

	"github.com/beadhub/bdh/internal/config"
)

func TestCanonicalizeOriginURL(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "SSH URL with .git suffix",
			origin: "git@github.com:beadhub/bdh.git",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "SSH URL without .git suffix",
			origin: "git@github.com:beadhub/bdh",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "HTTPS URL with .git suffix",
			origin: "https://github.com/beadhub/bdh.git",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "HTTPS URL without .git suffix",
			origin: "https://github.com/beadhub/bdh",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "HTTP URL",
			origin: "http://github.com/beadhub/bdh",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "URL with trailing slash",
			origin: "https://github.com/beadhub/bdh/",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "URL with mixed case host",
			origin: "https://GitHub.COM/beadhub/bdh",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "empty string",
			origin: "",
			want:   "",
		},
		{
			name:   "whitespace only",
			origin: "   ",
			want:   "",
		},
		{
			name:   "malformed SSH URL (no colon)",
			origin: "git@github.com/beadhub/bdh",
			want:   "",
		},
		{
			name:   "SSH URL with empty host",
			origin: "git@:path/to/repo",
			want:   "",
		},
		{
			name:   "SSH URL with empty path",
			origin: "git@github.com:",
			want:   "",
		},
		{
			name:   "HTTPS URL with no path",
			origin: "https://github.com",
			want:   "",
		},
		{
			name:   "HTTPS URL with slash only path",
			origin: "https://github.com/",
			want:   "",
		},
		{
			name:   "leading whitespace",
			origin: "  git@github.com:beadhub/bdh.git",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "trailing whitespace",
			origin: "git@github.com:beadhub/bdh.git  ",
			want:   "github.com/beadhub/bdh",
		},
		{
			name:   "GitLab SSH",
			origin: "git@gitlab.com:group/project.git",
			want:   "gitlab.com/group/project",
		},
		{
			name:   "Bitbucket SSH",
			origin: "git@bitbucket.org:workspace/repo.git",
			want:   "bitbucket.org/workspace/repo",
		},
		{
			name:   "nested path SSH",
			origin: "git@github.com:org/group/subgroup/repo.git",
			want:   "github.com/org/group/subgroup/repo",
		},
		{
			name:   "nested path HTTPS",
			origin: "https://gitlab.com/org/group/subgroup/repo.git",
			want:   "gitlab.com/org/group/subgroup/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalizeOriginURL(tt.origin)
			if got != tt.want {
				t.Errorf("canonicalizeOriginURL(%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}

func TestIsGitNotFoundOrNotRepo(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "exec.ErrNotFound",
			err:  exec.ErrNotFound,
			want: true,
		},
		{
			name: "generic error",
			err:  &exec.Error{Name: "git", Err: exec.ErrNotFound},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitNotFoundOrNotRepo(tt.err)
			if got != tt.want {
				t.Errorf("isGitNotFoundOrNotRepo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateRepoOriginMatchesCurrent_SkipEnvVar(t *testing.T) {
	// Test that BEADHUB_SKIP_REPO_CHECK=1 skips validation
	t.Setenv("BEADHUB_SKIP_REPO_CHECK", "1")
	t.Setenv("BEADHUB_REPO_ORIGIN", "")

	cfg := &config.Config{
		CanonicalOrigin: "github.com/different/repo",
	}

	err := validateRepoOriginMatchesCurrent(cfg)
	if err != nil {
		t.Errorf("expected no error with skip env var, got: %v", err)
	}
}

func TestValidateRepoOriginMatchesCurrent_MatchingOrigin(t *testing.T) {
	t.Setenv("BEADHUB_SKIP_REPO_CHECK", "")
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:beadhub/bdh.git")

	cfg := &config.Config{
		CanonicalOrigin: "github.com/beadhub/bdh",
	}

	err := validateRepoOriginMatchesCurrent(cfg)
	if err != nil {
		t.Errorf("expected no error with matching origin, got: %v", err)
	}
}

func TestValidateRepoOriginMatchesCurrent_MismatchedOrigin(t *testing.T) {
	t.Setenv("BEADHUB_SKIP_REPO_CHECK", "")
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:beadhub/bdh.git")

	cfg := &config.Config{
		CanonicalOrigin: "github.com/other/repo",
	}

	err := validateRepoOriginMatchesCurrent(cfg)
	if err == nil {
		t.Errorf("expected error with mismatched origin, got nil")
	}
}

func TestValidateRepoOriginMatchesCurrent_EmptyConfigOrigin(t *testing.T) {
	t.Setenv("BEADHUB_SKIP_REPO_CHECK", "")
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:beadhub/bdh.git")

	cfg := &config.Config{
		CanonicalOrigin: "",
	}

	// Empty config origin should skip validation
	err := validateRepoOriginMatchesCurrent(cfg)
	if err != nil {
		t.Errorf("expected no error with empty config origin, got: %v", err)
	}
}

func TestValidateRepoOriginMatchesCurrent_InvalidCurrentOrigin(t *testing.T) {
	t.Setenv("BEADHUB_SKIP_REPO_CHECK", "")
	t.Setenv("BEADHUB_REPO_ORIGIN", "invalid-not-a-url")

	cfg := &config.Config{
		CanonicalOrigin: "github.com/beadhub/bdh",
	}

	// Invalid origin canonicalizes to empty, should skip validation
	err := validateRepoOriginMatchesCurrent(cfg)
	if err != nil {
		t.Errorf("expected no error with invalid current origin, got: %v", err)
	}
}
