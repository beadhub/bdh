package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBeadhubAuth_FromGlobalConfigAndContext(t *testing.T) {
	tmp := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfgPath := filepath.Join(tmp, "awconfig.yaml")
	t.Setenv("AW_CONFIG_PATH", cfgPath)

	if err := os.MkdirAll(filepath.Join(tmp, ".aw"), 0o700); err != nil {
		t.Fatalf("mkdir .aw: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".aw", "context"), []byte("default_account: acct\n"), 0o600); err != nil {
		t.Fatalf("write context: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte(`
servers:
  beadhub:
    url: http://localhost:8000
accounts:
  acct:
    server: beadhub
    api_key: aw_sk_test
    agent_id: agent-1
    agent_alias: alice
default_account: acct
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	if sel.BaseURL != "http://localhost:8000" {
		t.Fatalf("baseURL=%q", sel.BaseURL)
	}
	if sel.APIKey != "aw_sk_test" {
		t.Fatalf("apiKey=%q", sel.APIKey)
	}
	if sel.AgentID != "agent-1" {
		t.Fatalf("agentID=%q", sel.AgentID)
	}
	if sel.AgentAlias != "alice" {
		t.Fatalf("agentAlias=%q", sel.AgentAlias)
	}
}

func TestResolveBeadhubAuth_AllowsEnvOnly(t *testing.T) {
	t.Setenv("BEADHUB_URL", "http://localhost:8000")
	t.Setenv("BEADHUB_API_KEY", "aw_sk_env")

	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	if sel.BaseURL != "http://localhost:8000" {
		t.Fatalf("baseURL=%q", sel.BaseURL)
	}
	if sel.APIKey != "aw_sk_env" {
		t.Fatalf("apiKey=%q", sel.APIKey)
	}
}
