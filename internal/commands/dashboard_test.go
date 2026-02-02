package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/awebai/aw/awconfig"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func TestDashboard_UsesAccountWhenEnvKeyMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmp, "aw-config.yaml"))
	t.Setenv("BEADHUB_API_KEY", "")

	wd := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(wd, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(wd); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	serverName := "localhost:8000"
	accountName := "acct-local"
	apiKey := "aw_sk_test_123456789012345678901234567890123456"

	if err := awconfig.UpdateGlobalAt(os.Getenv("AW_CONFIG_PATH"), func(cfg *awconfig.GlobalConfig) error {
		if cfg.Servers == nil {
			cfg.Servers = map[string]awconfig.Server{}
		}
		if cfg.Accounts == nil {
			cfg.Accounts = map[string]awconfig.Account{}
		}
		cfg.Servers[serverName] = awconfig.Server{URL: "http://localhost:8000"}
		cfg.Accounts[accountName] = awconfig.Account{
			Server:         serverName,
			APIKey:         apiKey,
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

	origDashboardURL := dashboardURL
	origDashboardOpen := dashboardOpen
	t.Cleanup(func() {
		dashboardURL = origDashboardURL
		dashboardOpen = origDashboardOpen
	})
	dashboardURL = "http://example.test/"
	dashboardOpen = false

	var gotErr error
	out := captureStdout(t, func() {
		gotErr = runDashboard(nil, []string{})
	})
	if gotErr != nil {
		t.Fatalf("runDashboard: %v", gotErr)
	}
	if !strings.Contains(out, "api_key="+apiKey) {
		t.Fatalf("expected api_key in output, got: %q", out)
	}
}
