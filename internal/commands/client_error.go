package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/beadhub/bdh/internal/client"
)

func formatClientErr(err error) error {
	var clientErr *client.Error
	if !errors.As(err, &clientErr) {
		return err
	}
	if strings.TrimSpace(clientErr.Hint) == "" {
		return fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
	}
	return fmt.Errorf(
		"BeadHub error (%d): %s\n\nHint: %s",
		clientErr.StatusCode,
		clientErr.Body,
		strings.TrimSpace(clientErr.Hint),
	)
}

