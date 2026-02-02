package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/config"
)

func persistBeadhubAccountAndContext(beadhubURL, projectSlug, alias, apiKey, agentID string) (accountName string, serverName string, err error) {
	serverName, err = awconfig.DeriveServerNameFromURL(beadhubURL)
	if err != nil {
		return "", "", fmt.Errorf("derive server name: %w", err)
	}
	accountName = deriveAccountName(serverName, projectSlug, alias)

	if err := awconfig.UpdateGlobal(func(cfg *awconfig.GlobalConfig) error {
		if cfg.Servers == nil {
			cfg.Servers = map[string]awconfig.Server{}
		}
		if cfg.Accounts == nil {
			cfg.Accounts = map[string]awconfig.Account{}
		}
		cfg.Servers[serverName] = awconfig.Server{URL: beadhubURL}
		cfg.Accounts[accountName] = awconfig.Account{
			Server:         serverName,
			APIKey:         apiKey,
			DefaultProject: projectSlug,
			AgentID:        agentID,
			AgentAlias:     alias,
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
