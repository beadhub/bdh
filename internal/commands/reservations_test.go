package commands

import (
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/client"
)

func strPtr(s string) *string {
	return &s
}

func TestFormatReservationsOutput_NoReservations(t *testing.T) {
	reservationsMine = false

	result := &ReservationsResult{
		Reservations: nil,
		Count:        0,
		MyAlias:      "claude-coord",
	}

	output := formatReservationsOutput(result, false)

	if !strings.Contains(output, "No active reservations in the project") {
		t.Errorf("Expected output to indicate no reservations, got: %s", output)
	}
}

func TestFormatReservationsOutput_NoReservationsMineFlagSet(t *testing.T) {
	reservationsMine = true

	result := &ReservationsResult{
		Reservations: nil,
		Count:        0,
		MyAlias:      "claude-coord",
	}

	output := formatReservationsOutput(result, false)

	if !strings.Contains(output, "You have no active reservations") {
		t.Errorf("Expected output to indicate no personal reservations, got: %s", output)
	}

	// Reset flag
	reservationsMine = false
}

func TestFormatReservationsOutput_WithReservations(t *testing.T) {
	result := &ReservationsResult{
		Reservations: []client.LockInfo{
			{
				ReservationID:       "res_123",
				Path:                "src/api.py",
				Alias:               "claude-be",
				TTLRemainingSeconds: 180,
				Reason:              strPtr("Working on auth"),
			},
			{
				ReservationID:       "res_456",
				Path:                "src/models.py",
				Alias:               "claude-coord",
				TTLRemainingSeconds: 300,
			},
		},
		Count:   2,
		MyAlias: "claude-coord",
	}

	output := formatReservationsOutput(result, false)

	// Own reservations should be in "Your Reservations" section
	if !strings.Contains(output, "## Your Reservations") {
		t.Errorf("Expected output to contain Your Reservations header, got: %s", output)
	}
	if !strings.Contains(output, "`src/models.py`") {
		t.Errorf("Expected output to contain own reservation path, got: %s", output)
	}
	// Other agents' reservations should be in "Other Agents' Reservations" section
	if !strings.Contains(output, "## Other Agents' Reservations") {
		t.Errorf("Expected output to contain Other Agents header, got: %s", output)
	}
	if !strings.Contains(output, "`src/api.py` — claude-be") {
		t.Errorf("Expected output to contain other agent's reservation, got: %s", output)
	}
	if !strings.Contains(output, "Working on auth") {
		t.Errorf("Expected output to contain reason, got: %s", output)
	}
}

func TestFormatReservationsOutput_JSON(t *testing.T) {
	result := &ReservationsResult{
		Reservations: []client.LockInfo{
			{
				ReservationID: "res_123",
				Path:          "src/api.py",
				Alias:         "claude-be",
			},
		},
		Count:   1,
		MyAlias: "claude-coord",
	}

	output := formatReservationsOutput(result, true)

	if !strings.Contains(output, `"reservation_id": "res_123"`) {
		t.Errorf("Expected JSON output with reservation_id, got: %s", output)
	}
	if !strings.Contains(output, `"count": 1`) {
		t.Errorf("Expected JSON output with count, got: %s", output)
	}
}

func TestFormatReservationsOutput_Warning(t *testing.T) {
	result := &ReservationsResult{
		Warning: "BeadHub unreachable at http://localhost:8000",
	}

	output := formatReservationsOutput(result, false)

	if !strings.Contains(output, "Warning: BeadHub unreachable") {
		t.Errorf("Expected output to contain warning, got: %s", output)
	}
}

func TestFormatReservationsOutput_JSONWarning(t *testing.T) {
	result := &ReservationsResult{
		Warning: "BeadHub unreachable at http://localhost:8000",
	}

	output := formatReservationsOutput(result, true)

	if !strings.Contains(output, `"warning": "BeadHub unreachable at http://localhost:8000"`) {
		t.Errorf("Expected JSON output to contain warning, got: %s", output)
	}
}
