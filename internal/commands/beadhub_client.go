package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/client"
)

type beadhubAuthSelection struct {
	BaseURL     string
	APIKey      string
	AccountName string
	ServerName  string

	DefaultProject string
	AgentID        string
	AgentAlias     string
}

func apiKeyFromEnv() string {
	return strings.TrimSpace(os.Getenv("BEADHUB_API_KEY"))
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
	return client.New(resolveConfig("", "BEADHUB_URL", "http://localhost:8000"))
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
