package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/awebai/aw/awconfig"
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
    url: https://app.beadhub.ai/api
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
	if sel.BaseURL != "https://app.beadhub.ai/api" {
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
	t.Setenv("BEADHUB_URL", "https://app.beadhub.ai/api")
	t.Setenv("BEADHUB_API_KEY", "aw_sk_env")

	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	if sel.BaseURL != "https://app.beadhub.ai/api" {
		t.Fatalf("baseURL=%q", sel.BaseURL)
	}
	if sel.APIKey != "aw_sk_env" {
		t.Fatalf("apiKey=%q", sel.APIKey)
	}
}

// ---------------------------------------------------------------------------
// findAccountForWorkspace unit tests
// ---------------------------------------------------------------------------

func TestFindAccountForWorkspace_DeterministicName(t *testing.T) {
	global := &awconfig.GlobalConfig{
		Accounts: map[string]awconfig.Account{
			"acct-srv__proj__alice": {Server: "srv", APIKey: "key1", AgentID: "ws-1", AgentAlias: "alice", DefaultProject: "proj"},
			"acct-srv__proj__bob":   {Server: "srv", APIKey: "key2", AgentID: "ws-2", AgentAlias: "bob", DefaultProject: "proj"},
		},
	}
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-1", Alias: "alice", ProjectSlug: "proj"}

	name, srv, ok := findAccountForWorkspace(global, hint, "srv")
	if !ok {
		t.Fatal("expected match")
	}
	if name != "acct-srv__proj__alice" {
		t.Fatalf("name=%q", name)
	}
	if srv != "srv" {
		t.Fatalf("server=%q", srv)
	}
}

func TestFindAccountForWorkspace_AgentIDMatch(t *testing.T) {
	// No deterministic name, but agent_id matches exactly one account.
	global := &awconfig.GlobalConfig{
		Accounts: map[string]awconfig.Account{
			"legacy-name": {Server: "srv", APIKey: "key1", AgentID: "ws-1", AgentAlias: "alice"},
		},
	}
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-1", Alias: "alice", ProjectSlug: "proj"}

	name, srv, ok := findAccountForWorkspace(global, hint, "srv")
	if !ok {
		t.Fatal("expected match")
	}
	if name != "legacy-name" {
		t.Fatalf("name=%q", name)
	}
	if srv != "srv" {
		t.Fatalf("server=%q", srv)
	}
}

func TestFindAccountForWorkspace_AmbiguousAgentID(t *testing.T) {
	// Two accounts with the same agent_id → ambiguous, should return false.
	global := &awconfig.GlobalConfig{
		Accounts: map[string]awconfig.Account{
			"acct-a": {Server: "srv", APIKey: "key1", AgentID: "ws-1", AgentAlias: "alice"},
			"acct-b": {Server: "srv", APIKey: "key2", AgentID: "ws-1", AgentAlias: "alice"},
		},
	}
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-1", Alias: "alice", ProjectSlug: "proj"}

	_, _, ok := findAccountForWorkspace(global, hint, "srv")
	if ok {
		t.Fatal("expected no match on ambiguous agent_id")
	}
}

func TestFindAccountForWorkspace_NoMatchingAccount(t *testing.T) {
	global := &awconfig.GlobalConfig{
		Accounts: map[string]awconfig.Account{
			"acct-bob": {Server: "srv", APIKey: "key1", AgentID: "ws-bob", AgentAlias: "bob"},
		},
	}
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-alice", Alias: "alice", ProjectSlug: "proj"}

	_, _, ok := findAccountForWorkspace(global, hint, "srv")
	if ok {
		t.Fatal("expected no match")
	}
}

func TestFindAccountForWorkspace_NilInputs(t *testing.T) {
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-1", Alias: "alice"}

	if _, _, ok := findAccountForWorkspace(nil, hint, "srv"); ok {
		t.Fatal("expected false for nil global")
	}
	global := &awconfig.GlobalConfig{Accounts: map[string]awconfig.Account{}}
	if _, _, ok := findAccountForWorkspace(global, nil, "srv"); ok {
		t.Fatal("expected false for nil hint")
	}
}

func TestFindAccountForWorkspace_SkipsAccountsWithoutAPIKey(t *testing.T) {
	global := &awconfig.GlobalConfig{
		Accounts: map[string]awconfig.Account{
			"no-key": {Server: "srv", APIKey: "", AgentID: "ws-1", AgentAlias: "alice"},
		},
	}
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-1", Alias: "alice", ProjectSlug: "proj"}

	_, _, ok := findAccountForWorkspace(global, hint, "srv")
	if ok {
		t.Fatal("expected no match for account without API key")
	}
}

func TestFindAccountForWorkspace_AliasFallback(t *testing.T) {
	// No deterministic name, no agent_id match, but alias+server+project match.
	global := &awconfig.GlobalConfig{
		Accounts: map[string]awconfig.Account{
			"some-name": {Server: "srv", APIKey: "key1", AgentID: "different-ws", AgentAlias: "alice", DefaultProject: "proj"},
		},
	}
	hint := &beadhubWorkspaceHint{WorkspaceID: "ws-1", Alias: "alice", ProjectSlug: "proj"}

	name, _, ok := findAccountForWorkspace(global, hint, "srv")
	if !ok {
		t.Fatal("expected alias fallback match")
	}
	if name != "some-name" {
		t.Fatalf("name=%q", name)
	}
}

// ---------------------------------------------------------------------------
// resolveBeadhubAuth integration tests
// ---------------------------------------------------------------------------

func TestResolveBeadhubAuth_PrefersBeadhubWorkspaceIdentityAndRepairsContext(t *testing.T) {
	tmp := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Make config.Load() treat tmp as a git root.
	if err := os.WriteFile(filepath.Join(tmp, ".git"), []byte("gitdir: /dev/null\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}

	// BeadHub identity for this worktree (authoritative for bdh commands).
	if err := os.WriteFile(filepath.Join(tmp, ".beadhub"), []byte(`
workspace_id: "ws-olivia"
beadhub_url: "https://app.beadhub.ai/api"
project_slug: "beadhub"
repo_origin: "git@github.com:awebai/aw.git"
canonical_origin: "github.com/awebai/aw"
alias: "olivia"
human_name: "Olivia"
role: "developer"
`), 0o600); err != nil {
		t.Fatalf("write .beadhub: %v", err)
	}

	cfgPath := filepath.Join(tmp, "awconfig.yaml")
	t.Setenv("AW_CONFIG_PATH", cfgPath)

	if err := os.MkdirAll(filepath.Join(tmp, ".aw"), 0o700); err != nil {
		t.Fatalf("mkdir .aw: %v", err)
	}
	// Wrong context: points at the noah account.
	if err := os.WriteFile(filepath.Join(tmp, ".aw", "context"), []byte(`
default_account: acct-app.beadhub.ai__beadhub__noah
server_accounts:
  app.beadhub.ai: acct-app.beadhub.ai__beadhub__noah
`), 0o600); err != nil {
		t.Fatalf("write context: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte(`
servers:
  app.beadhub.ai:
    url: https://app.beadhub.ai/api
accounts:
  acct-app.beadhub.ai__beadhub__noah:
    server: app.beadhub.ai
    api_key: aw_sk_noah
    default_project: beadhub
    agent_id: ws-noah
    agent_alias: noah
  acct-app.beadhub.ai__beadhub__olivia:
    server: app.beadhub.ai
    api_key: aw_sk_olivia
    default_project: beadhub
    agent_id: ws-olivia
    agent_alias: olivia
default_account: acct-app.beadhub.ai__beadhub__noah
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	if sel.AgentAlias != "olivia" {
		t.Fatalf("agentAlias=%q", sel.AgentAlias)
	}
	if sel.AccountName != "acct-app.beadhub.ai__beadhub__olivia" {
		t.Fatalf("accountName=%q", sel.AccountName)
	}
	if sel.APIKey != "aw_sk_olivia" {
		t.Fatalf("apiKey=%q", sel.APIKey)
	}

	// Context should be repaired to match .beadhub identity.
	ctx, err := awconfig.LoadWorktreeContextFrom(filepath.Join(tmp, ".aw", "context"))
	if err != nil {
		t.Fatalf("LoadWorktreeContextFrom: %v", err)
	}
	if ctx.DefaultAccount != "acct-app.beadhub.ai__beadhub__olivia" {
		t.Fatalf("context default_account=%q", ctx.DefaultAccount)
	}
	if ctx.ServerAccounts["app.beadhub.ai"] != "acct-app.beadhub.ai__beadhub__olivia" {
		t.Fatalf("context server_accounts[app.beadhub.ai]=%q", ctx.ServerAccounts["app.beadhub.ai"])
	}
}

// Safety: no .beadhub → regular awconfig.Resolve path, no side effects.
func TestResolveBeadhubAuth_NoBeadhubFile_UsesRegularResolve(t *testing.T) {
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
    url: https://app.beadhub.ai/api
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

	// No .beadhub file → should still resolve via regular path.
	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	if sel.APIKey != "aw_sk_test" {
		t.Fatalf("apiKey=%q", sel.APIKey)
	}
	if sel.AgentAlias != "alice" {
		t.Fatalf("agentAlias=%q", sel.AgentAlias)
	}

	// Context should be unchanged.
	ctx, err := awconfig.LoadWorktreeContextFrom(filepath.Join(tmp, ".aw", "context"))
	if err != nil {
		t.Fatalf("LoadWorktreeContextFrom: %v", err)
	}
	if ctx.DefaultAccount != "acct" {
		t.Fatalf("context modified: default_account=%q", ctx.DefaultAccount)
	}
}

// Safety: .beadhub exists but no matching aw account → falls through gracefully.
func TestResolveBeadhubAuth_BeadhubNoMatchingAccount_FallsThrough(t *testing.T) {
	tmp := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".git"), []byte("gitdir: /dev/null\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}

	// .beadhub says we're "olivia" but aw config only has "bob".
	if err := os.WriteFile(filepath.Join(tmp, ".beadhub"), []byte(`
workspace_id: "ws-olivia"
beadhub_url: "https://app.beadhub.ai/api"
project_slug: "proj"
alias: "olivia"
`), 0o600); err != nil {
		t.Fatalf("write .beadhub: %v", err)
	}

	cfgPath := filepath.Join(tmp, "awconfig.yaml")
	t.Setenv("AW_CONFIG_PATH", cfgPath)

	if err := os.MkdirAll(filepath.Join(tmp, ".aw"), 0o700); err != nil {
		t.Fatalf("mkdir .aw: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".aw", "context"), []byte("default_account: acct-bob\n"), 0o600); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`
servers:
  app.beadhub.ai:
    url: https://app.beadhub.ai/api
accounts:
  acct-bob:
    server: app.beadhub.ai
    api_key: aw_sk_bob
    agent_id: ws-bob
    agent_alias: bob
default_account: acct-bob
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	// Falls through to regular resolve → picks context default (bob).
	if sel.APIKey != "aw_sk_bob" {
		t.Fatalf("apiKey=%q", sel.APIKey)
	}
	if sel.AgentAlias != "bob" {
		t.Fatalf("agentAlias=%q", sel.AgentAlias)
	}

	// Context should be unchanged — no repair attempted.
	ctx, err := awconfig.LoadWorktreeContextFrom(filepath.Join(tmp, ".aw", "context"))
	if err != nil {
		t.Fatalf("LoadWorktreeContextFrom: %v", err)
	}
	if ctx.DefaultAccount != "acct-bob" {
		t.Fatalf("context modified: default_account=%q", ctx.DefaultAccount)
	}
}

// Safety: BEADHUB_API_KEY env set → .beadhub path is skipped entirely.
func TestResolveBeadhubAuth_EnvAPIKey_SkipsBeadhubLookup(t *testing.T) {
	tmp := t.TempDir()

	orig, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".git"), []byte("gitdir: /dev/null\n"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".beadhub"), []byte(`
workspace_id: "ws-olivia"
beadhub_url: "https://app.beadhub.ai/api"
project_slug: "proj"
alias: "olivia"
`), 0o600); err != nil {
		t.Fatalf("write .beadhub: %v", err)
	}

	cfgPath := filepath.Join(tmp, "awconfig.yaml")
	t.Setenv("AW_CONFIG_PATH", cfgPath)
	t.Setenv("BEADHUB_API_KEY", "aw_sk_override")

	if err := os.MkdirAll(filepath.Join(tmp, ".aw"), 0o700); err != nil {
		t.Fatalf("mkdir .aw: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".aw", "context"), []byte("default_account: acct-bob\n"), 0o600); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(`
servers:
  app.beadhub.ai:
    url: https://app.beadhub.ai/api
accounts:
  acct-bob:
    server: app.beadhub.ai
    api_key: aw_sk_bob
    agent_id: ws-bob
    agent_alias: bob
  acct-olivia:
    server: app.beadhub.ai
    api_key: aw_sk_olivia
    agent_id: ws-olivia
    agent_alias: olivia
default_account: acct-bob
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	sel, err := resolveBeadhubAuth("")
	if err != nil {
		t.Fatalf("resolveBeadhubAuth: %v", err)
	}
	// Env API key wins, .beadhub not consulted.
	if sel.APIKey != "aw_sk_override" {
		t.Fatalf("apiKey=%q, want aw_sk_override", sel.APIKey)
	}

	// Context should be unchanged.
	ctx, err := awconfig.LoadWorktreeContextFrom(filepath.Join(tmp, ".aw", "context"))
	if err != nil {
		t.Fatalf("LoadWorktreeContextFrom: %v", err)
	}
	if ctx.DefaultAccount != "acct-bob" {
		t.Fatalf("context modified: default_account=%q", ctx.DefaultAccount)
	}
}
