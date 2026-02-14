package commands

import "time"

const (
	defaultReadyTeamLimit            = 15
	defaultReadyLocksLimit           = 10
	defaultSendAliasLimit            = 10
	readyTeamQueryOverflow           = 1
	maxWorkspaceQueryLimit           = 200
	defaultStatusTeamLimit           = 50
	defaultStatusTeamReservationsMax = 5 // Max reservations shown per team member

	// Workspace staleness: controls which team members appear in :status and ready output.
	// A workspace is considered recently active if EITHER its FocusUpdatedAt OR LastSeen
	// timestamp falls within this window. OR logic is intentional: an agent may set focus
	// once and then work silently for hours (LastSeen keeps updating via heartbeats even
	// when focus doesn't change). Workspaces with no parseable timestamps are included
	// (conservative — avoids hiding active agents with bad data). 6 hours accommodates
	// long-running sessions without showing workspaces that have been idle overnight.
	// See isWorkspaceRecentlyActive() in passthrough.go for the full check.
	teamActivityThresholdHours = 6
)

// teamActivityThreshold returns the time threshold for considering an agent recently active.
func teamActivityThreshold() time.Time {
	return time.Now().Add(-teamActivityThresholdHours * time.Hour)
}
