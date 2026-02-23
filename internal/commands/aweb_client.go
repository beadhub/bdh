package commands

import (
	"fmt"
	"os"
	"strings"

	aweb "github.com/awebai/aw"
	"github.com/awebai/aw/awconfig"
)

// newIdentityClient creates an identity-capable aweb client when a signing key
// and DID are configured. Returns nil if identity is not available.
func newIdentityClient(sel *beadhubAuthSelection) *aweb.Client {
	if sel.SigningKey == "" || sel.DID == "" {
		return nil
	}
	key, err := awconfig.LoadSigningKey(sel.SigningKey)
	if err != nil {
		return nil
	}
	c, err := aweb.NewWithIdentity(sel.BaseURL, sel.APIKey, key, sel.DID)
	if err != nil {
		return nil
	}
	if sel.NamespaceSlug != "" && sel.AgentAlias != "" {
		c.SetAddress(sel.NamespaceSlug + "/" + sel.AgentAlias)
	}
	return c
}

func newAwebClient(beadhubURL string) (*aweb.Client, error) {
	sel, err := resolveBeadhubAuth(beadhubURL)
	if err == nil && strings.TrimSpace(sel.BaseURL) != "" && strings.TrimSpace(sel.APIKey) != "" {
		if c := newIdentityClient(sel); c != nil {
			return c, nil
		}
		return aweb.NewWithAPIKey(sel.BaseURL, sel.APIKey)
	}
	baseURL := strings.TrimSpace(beadhubURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("BEADHUB_URL"))
	}
	if baseURL == "" && err == nil {
		baseURL = strings.TrimSpace(sel.BaseURL)
	}
	if baseURL == "" {
		baseURL = defaultBeadhubURL
	}
	return aweb.New(baseURL)
}

func newAwebClientRequired(beadhubURL string) (*aweb.Client, error) {
	sel, err := resolveBeadhubAuth(beadhubURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sel.APIKey) == "" {
		return nil, fmt.Errorf("missing beadhub API key (configure ~/.config/aw/config.yaml + .aw/context, or set BEADHUB_API_KEY)")
	}
	if c := newIdentityClient(sel); c != nil {
		return c, nil
	}
	return aweb.NewWithAPIKey(sel.BaseURL, sel.APIKey)
}
