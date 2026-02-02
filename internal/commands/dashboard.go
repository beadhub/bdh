package commands

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/config"
)

var (
	dashboardURL  string
	dashboardOpen bool
)

var dashboardCmd = &cobra.Command{
	Use:   ":dashboard",
	Short: "Open the dashboard with your API key",
	Long: `Open the BeadHub dashboard and authenticate using your project API key.

This command constructs a one-time login URL that includes the API key in the
URL fragment (after '#'), so it is not sent to the server. The dashboard stores
the key in your browser's localStorage for future sessions.

By default, this command prints the URL and opens it in your browser.`,
	RunE: runDashboard,
}

func init() {
	dashboardCmd.Flags().StringVar(
		&dashboardURL,
		"dashboard-url",
		"",
		"Dashboard URL (default: BEADHUB_DASHBOARD_URL or BEADHUB_URL origin or http://localhost:8000/)",
	)
	dashboardCmd.Flags().BoolVar(
		&dashboardOpen,
		"open",
		true,
		"Open the URL in your browser (prints URL regardless)",
	)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf(":dashboard takes no arguments")
	}

	apiKey := strings.TrimSpace(os.Getenv("BEADHUB_API_KEY"))
	baseURLFromSelection := ""
	if apiKey == "" {
		if sel, err := resolveBeadhubAuth(""); err == nil {
			apiKey = strings.TrimSpace(sel.APIKey)
			baseURLFromSelection = strings.TrimSpace(sel.BaseURL)
		}
	}
	if apiKey == "" {
		return fmt.Errorf("no API key found (run `bdh :init`, or configure ~/.config/aw/config.yaml + .aw/context, or set BEADHUB_API_KEY)")
	}

	base := strings.TrimSpace(dashboardURL)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("BEADHUB_DASHBOARD_URL"))
	}
	if base == "" {
		// Default to the API origin (common in Docker/prod where the backend serves the UI),
		// otherwise fall back to the standard OSS port.
		if cfg, err := config.Load(); err == nil && strings.TrimSpace(cfg.BeadhubURL) != "" {
			beadhubURL := strings.TrimSpace(cfg.BeadhubURL)
			if parsed, err := url.Parse(beadhubURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
				parsed.Path = ""
				parsed.RawQuery = ""
				parsed.Fragment = ""
				base = parsed.String()
			}
		}
		beadhubURL := strings.TrimSpace(os.Getenv("BEADHUB_URL"))
		if beadhubURL != "" {
			if parsed, err := url.Parse(beadhubURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
				parsed.Path = ""
				parsed.RawQuery = ""
				parsed.Fragment = ""
				base = parsed.String()
			}
		}
		if base == "" && baseURLFromSelection != "" {
			base = baseURLFromSelection
		}
		if base == "" {
			base = "http://localhost:8000/"
		}
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return fmt.Errorf("invalid dashboard URL: %q", base)
	}

	// Store the key in the URL fragment so it is not sent to the server.
	loginURL := base
	fragment := url.Values{}
	fragment.Set("api_key", apiKey)
	if strings.Contains(loginURL, "#") {
		loginURL = strings.SplitN(loginURL, "#", 2)[0]
	}
	loginURL = strings.TrimSuffix(loginURL, "#")
	loginURL = loginURL + "#" + fragment.Encode()

	fmt.Println(loginURL)

	if dashboardOpen {
		if err := openURL(loginURL); err != nil {
			// Non-fatal: printing the URL is always sufficient.
			fmt.Fprintf(os.Stderr, "Warning: failed to open browser: %v\n", err)
		}
	}

	return nil
}

func openURL(u string) error {
	var openCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		openCmd = exec.Command("open", u)
	case "windows":
		openCmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		openCmd = exec.Command("xdg-open", u)
	}
	return openCmd.Start()
}
