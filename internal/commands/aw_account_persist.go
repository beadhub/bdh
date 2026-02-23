package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/config"
)

type persistAccountParams struct {
	BeadhubURL    string
	ProjectSlug   string
	Alias         string
	APIKey        string
	AgentID       string
	NamespaceSlug string
	DID           string
	Custody       string
	Lifetime      string
}

func persistBeadhubAccountAndContext(p persistAccountParams) (accountName string, serverName string, err error) {
	serverName, err = awconfig.DeriveServerNameFromURL(p.BeadhubURL)
	if err != nil {
		return "", "", fmt.Errorf("derive server name: %w", err)
	}
	accountName = deriveAccountName(serverName, p.ProjectSlug, p.Alias)

	if err := awconfig.UpdateGlobal(func(cfg *awconfig.GlobalConfig) error {
		if cfg.Servers == nil {
			cfg.Servers = map[string]awconfig.Server{}
		}
		if cfg.Accounts == nil {
			cfg.Accounts = map[string]awconfig.Account{}
		}
		cfg.Servers[serverName] = awconfig.Server{URL: p.BeadhubURL}
		cfg.Accounts[accountName] = awconfig.Account{
			Server:         serverName,
			APIKey:         p.APIKey,
			DefaultProject: p.ProjectSlug,
			AgentID:        p.AgentID,
			AgentAlias:     p.Alias,
			NamespaceSlug:  p.NamespaceSlug,
			DID:            p.DID,
			Custody:        p.Custody,
			Lifetime:       p.Lifetime,
		}
		if strings.TrimSpace(cfg.DefaultAccount) == "" {
			cfg.DefaultAccount = accountName
		}
		return nil
	}); err != nil {
		return "", "", err
	}

	root, rootErr := config.WorkspaceRoot()
	if rootErr != nil {
		root, _ = os.Getwd()
	}
	ctxPath := filepath.Join(root, awconfig.DefaultWorktreeContextRelativePath())

	existing, err := awconfig.LoadWorktreeContextFrom(ctxPath)
	if err == nil {
		if existing.ServerAccounts == nil {
			existing.ServerAccounts = map[string]string{}
		}
		existing.DefaultAccount = accountName
		existing.ServerAccounts[serverName] = accountName
		return accountName, serverName, awconfig.SaveWorktreeContextTo(ctxPath, existing)
	}

	ctx := &awconfig.WorktreeContext{
		DefaultAccount: accountName,
		ServerAccounts: map[string]string{serverName: accountName},
	}
	return accountName, serverName, awconfig.SaveWorktreeContextTo(ctxPath, ctx)
}
