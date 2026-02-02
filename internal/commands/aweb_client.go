package commands

import (
	"fmt"
	"os"
	"strings"

	aweb "github.com/awebai/aw"
)

func newAwebClient(beadhubURL string) (*aweb.Client, error) {
	sel, err := resolveBeadhubAuth(beadhubURL)
	if err == nil && strings.TrimSpace(sel.BaseURL) != "" && strings.TrimSpace(sel.APIKey) != "" {
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
		baseURL = "http://localhost:8000"
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
	return aweb.NewWithAPIKey(sel.BaseURL, sel.APIKey)
}
