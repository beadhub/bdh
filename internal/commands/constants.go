package commands

import "time"

const (
	defaultReadyTeamLimit            = 15
	defaultReadyLocksLimit           = 10
	defaultSendAliasLimit            = 10
	readyTeamQueryOverflow           = 1
	teamActivityThresholdHours       = 6  // Show agents active in last 6 hours
	maxWorkspaceQueryLimit           = 200
	defaultStatusTeamLimit           = 50
	defaultStatusTeamReservationsMax = 10 // Max reservations shown per team member
)

// teamActivityThreshold returns the time threshold for considering an agent recently active.
func teamActivityThreshold() time.Time {
	return time.Now().Add(-teamActivityThresholdHours * time.Hour)
}
