package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

type beadhubAuthSelection struct {
	BaseURL     string
	APIKey      string
	AccountName string
	ServerName  string

	DefaultProject string
	AgentID        string
	AgentAlias     string
	NamespaceSlug  string
	DID            string
	SigningKey     string
	Custody        string
	Lifetime       string
}

func apiKeyFromEnv() string {
	return strings.TrimSpace(os.Getenv("BEADHUB_API_KEY"))
}

type beadhubWorkspaceHint struct {
	WorkspaceID string
	Alias       string
	ProjectSlug string
	BeadhubURL  string
	RootDir     string
}

func loadBeadhubWorkspaceHintBestEffort() (*beadhubWorkspaceHint, error) {
	cfg, err := config.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	root, err := config.WorkspaceRoot()
	if err != nil {
		root, _ = os.Getwd()
	}

	hint := &beadhubWorkspaceHint{
		WorkspaceID: strings.TrimSpace(cfg.WorkspaceID),
		Alias:       strings.TrimSpace(cfg.Alias),
		ProjectSlug: strings.TrimSpace(cfg.ProjectSlug),
		BeadhubURL:  strings.TrimSpace(cfg.BeadhubURL),
		RootDir:     root,
	}
	if hint.WorkspaceID == "" || hint.Alias == "" {
		return nil, nil
	}
	return hint, nil
}

func findAccountForWorkspace(global *awconfig.GlobalConfig, hint *beadhubWorkspaceHint, serverNameHint string) (accountName string, serverName string, ok bool) {
	if global == nil || hint == nil || global.Accounts == nil {
		return "", "", false
	}

	// Prefer deterministic account name when possible.
	if strings.TrimSpace(serverNameHint) != "" && strings.TrimSpace(hint.ProjectSlug) != "" && strings.TrimSpace(hint.Alias) != "" {
		expected := deriveAccountName(serverNameHint, hint.ProjectSlug, hint.Alias)
		if a, exists := global.Accounts[expected]; exists && strings.TrimSpace(a.APIKey) != "" {
			srv := strings.TrimSpace(a.Server)
			if srv == "" {
				srv = strings.TrimSpace(serverNameHint)
			}
			return expected, srv, true
		}
	}

	// Next, match on workspace_id stored as awconfig account.agent_id (BeadHub convention).
	var matchedName string
	var matchedAcct awconfig.Account
	for name, a := range global.Accounts {
		if strings.TrimSpace(a.APIKey) == "" {
			continue
		}
		if strings.TrimSpace(a.AgentID) != hint.WorkspaceID {
			continue
		}
		if matchedName != "" {
			// Ambiguous; require deterministic name or context to disambiguate.
			return "", "", false
		}
		matchedName = name
		matchedAcct = a
	}
	if matchedName != "" {
		return matchedName, strings.TrimSpace(matchedAcct.Server), true
	}

	// Final fallback: match by alias (+ project slug when available) on the hinted server.
	if strings.TrimSpace(serverNameHint) == "" {
		return "", "", false
	}
	for name, a := range global.Accounts {
		if strings.TrimSpace(a.APIKey) == "" {
			continue
		}
		if strings.TrimSpace(a.Server) != serverNameHint {
			continue
		}
		if strings.TrimSpace(a.AgentAlias) != hint.Alias {
			continue
		}
		if hint.ProjectSlug != "" && strings.TrimSpace(a.DefaultProject) != hint.ProjectSlug {
			continue
		}
		if matchedName != "" {
			return "", "", false
		}
		matchedName = name
		matchedAcct = a
	}
	if matchedName != "" {
		return matchedName, strings.TrimSpace(matchedAcct.Server), true
	}

	return "", "", false
}

func ensureWorktreeContext(rootDir, serverName, accountName string) error {
	rootDir = strings.TrimSpace(rootDir)
	serverName = strings.TrimSpace(serverName)
	accountName = strings.TrimSpace(accountName)
	if rootDir == "" || serverName == "" || accountName == "" {
		return nil
	}

	ctxPath := filepath.Join(rootDir, awconfig.DefaultWorktreeContextRelativePath())
	ctx, err := awconfig.LoadWorktreeContextFrom(ctxPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		ctx = &awconfig.WorktreeContext{
			DefaultAccount: accountName,
			ServerAccounts: map[string]string{serverName: accountName},
		}
		return awconfig.SaveWorktreeContextTo(ctxPath, ctx)
	}
	if ctx.ServerAccounts == nil {
		ctx.ServerAccounts = map[string]string{}
	}
	changed := false
	// Avoid clobbering the directory's existing default_account, which may be used
	// by other clients (e.g. `aw`) to default to a different server. For BeadHub,
	// the authoritative identity comes from .beadhub and we select the account
	// explicitly, so we only need to ensure the per-server mapping exists.
	if strings.TrimSpace(ctx.DefaultAccount) == "" {
		ctx.DefaultAccount = accountName
		changed = true
	}
	if strings.TrimSpace(ctx.ServerAccounts[serverName]) != accountName {
		ctx.ServerAccounts[serverName] = accountName
		changed = true
	}
	if !changed {
		return nil
	}
	return awconfig.SaveWorktreeContextTo(ctxPath, ctx)
}

func resolveBeadhubAuth(beadhubURLHint string) (*beadhubAuthSelection, error) {
	urlOverride := strings.TrimSpace(os.Getenv("BEADHUB_URL"))
	if urlOverride == "" {
		urlOverride = strings.TrimSpace(beadhubURLHint)
	}
	keyOverride := apiKeyFromEnv()

	// Script/CI escape hatch: allow full env-only operation.
	if urlOverride != "" && keyOverride != "" {
		return &beadhubAuthSelection{
			BaseURL: urlOverride,
			APIKey:  keyOverride,
		}, nil
	}

	wd, _ := os.Getwd()
	global, err := awconfig.LoadGlobal()
	if err != nil {
		// If user provided a URL, allow unauthenticated client usage (e.g., before init).
		if urlOverride != "" {
			return &beadhubAuthSelection{BaseURL: urlOverride, APIKey: keyOverride}, nil
		}
		return nil, err
	}

	// If we're in a BeadHub-initialized worktree, prefer selecting the aw account
	// that matches the .beadhub identity, and keep .aw/context in sync. This is
	// skipped when BEADHUB_API_KEY is explicitly set.
	hint, hintErr := loadBeadhubWorkspaceHintBestEffort()
	if hintErr == nil && hint != nil && keyOverride == "" {
		effectiveURL := strings.TrimSpace(urlOverride)
		if effectiveURL == "" {
			effectiveURL = strings.TrimSpace(hint.BeadhubURL)
		}
		serverNameHint := ""
		if effectiveURL != "" {
			if sn, err := awconfig.DeriveServerNameFromURL(effectiveURL); err == nil {
				serverNameHint = sn
			}
		}
		if acctName, serverName, ok := findAccountForWorkspace(global, hint, serverNameHint); ok {
			_ = ensureWorktreeContext(hint.RootDir, serverName, acctName)
			if resolved, err := awconfig.Resolve(global, awconfig.ResolveOptions{
				AccountName:       acctName,
				WorkingDir:        wd,
				BaseURLOverride:   effectiveURL,
				APIKeyOverride:    keyOverride,
				AllowEnvOverrides: false,
			}); err == nil {
				return &beadhubAuthSelection{
					BaseURL:        resolved.BaseURL,
					APIKey:         resolved.APIKey,
					AccountName:    resolved.AccountName,
					ServerName:     resolved.ServerName,
					DefaultProject: resolved.DefaultProject,
					AgentID:        resolved.AgentID,
					AgentAlias:     resolved.AgentAlias,
					NamespaceSlug:  resolved.NamespaceSlug,
					DID:            resolved.DID,
					SigningKey:     resolved.SigningKey,
					Custody:        resolved.Custody,
					Lifetime:       resolved.Lifetime,
				}, nil
			}
		}
	}

	sel, err := awconfig.Resolve(global, awconfig.ResolveOptions{
		WorkingDir:        wd,
		BaseURLOverride:   urlOverride,
		APIKeyOverride:    keyOverride,
		AllowEnvOverrides: false,
	})
	if err != nil {
		// If user provided a URL, allow unauthenticated client usage (e.g., before init).
		if urlOverride != "" {
			return &beadhubAuthSelection{BaseURL: urlOverride, APIKey: keyOverride}, nil
		}
		return nil, err
	}
	return &beadhubAuthSelection{
		BaseURL:        sel.BaseURL,
		APIKey:         sel.APIKey,
		AccountName:    sel.AccountName,
		ServerName:     sel.ServerName,
		DefaultProject: sel.DefaultProject,
		AgentID:        sel.AgentID,
		AgentAlias:     sel.AgentAlias,
		NamespaceSlug:  sel.NamespaceSlug,
		DID:            sel.DID,
		SigningKey:     sel.SigningKey,
		Custody:        sel.Custody,
		Lifetime:       sel.Lifetime,
	}, nil
}

func newBeadHubClient(beadhubURL string) *client.Client {
	sel, err := resolveBeadhubAuth(beadhubURL)
	if err == nil && strings.TrimSpace(sel.APIKey) != "" {
		return client.NewWithAPIKey(sel.BaseURL, sel.APIKey)
	}
	if strings.TrimSpace(beadhubURL) != "" {
		return client.New(beadhubURL)
	}
	if err == nil && strings.TrimSpace(sel.BaseURL) != "" {
		return client.New(sel.BaseURL)
	}
	return client.New(resolveConfig("", "BEADHUB_URL", defaultBeadhubURL))
}

func newBeadHubClientRequired(beadhubURL string) (*client.Client, error) {
	sel, err := resolveBeadhubAuth(beadhubURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sel.APIKey) == "" {
		return nil, fmt.Errorf("missing beadhub API key (configure ~/.config/aw/config.yaml + .aw/context, or set BEADHUB_API_KEY)")
	}
	return client.NewWithAPIKey(sel.BaseURL, sel.APIKey), nil
}
