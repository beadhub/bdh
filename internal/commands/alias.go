package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

// MatchType describes how an alias was matched.
type MatchType int

const (
	MatchExact MatchType = iota
	MatchPrefix
	MatchSubstring
)

func (m MatchType) String() string {
	switch m {
	case MatchExact:
		return "exact"
	case MatchPrefix:
		return "prefix"
	case MatchSubstring:
		return "substring"
	default:
		return "unknown"
	}
}

// AliasMatch represents a matched workspace.
type AliasMatch struct {
	WorkspaceID string
	Alias       string
	HumanName   string
	MatchType   MatchType
}

// AliasResolution is the result of resolving an alias.
type AliasResolution struct {
	WorkspaceID string
	Alias       string
	MatchType   MatchType
}

// resolveAlias resolves a target string to a workspace.
// Resolution order:
// 1. Exact match
// 2. Unique prefix match
// 3. Unique substring match
// Returns error with suggestions if ambiguous or not found.
func resolveAlias(ctx context.Context, cfg *config.Config, httpClient *client.Client, target string) (*AliasResolution, error) {
	if target == "" {
		return nil, fmt.Errorf("target alias cannot be empty")
	}

	// If it's a UUID, return directly without resolution
	if isUUID(target) {
		return &AliasResolution{
			WorkspaceID: target,
			Alias:       "",
			MatchType:   MatchExact,
		}, nil
	}

	// Fetch all workspaces for this project (not just active ones with claims)
	includePresence := false
	resp, err := httpClient.Workspaces(ctx, &client.WorkspacesRequest{
		IncludePresence: &includePresence,
		Limit:           defaultWhoMaxLimit, // Get all workspaces for matching
	})
	if err != nil {
		return nil, fmt.Errorf("fetching workspaces: %w", err)
	}

	if len(resp.Workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces found in project %q", cfg.ProjectSlug)
	}

	// Try matching in order of priority
	targetLower := strings.ToLower(target)

	// 1. Exact match
	var exactMatches []AliasMatch
	for _, ws := range resp.Workspaces {
		if strings.ToLower(ws.Alias) == targetLower {
			exactMatches = append(exactMatches, AliasMatch{
				WorkspaceID: ws.WorkspaceID,
				Alias:       ws.Alias,
				HumanName:   ws.HumanName,
				MatchType:   MatchExact,
			})
		}
	}

	if len(exactMatches) == 1 {
		return &AliasResolution{
			WorkspaceID: exactMatches[0].WorkspaceID,
			Alias:       exactMatches[0].Alias,
			MatchType:   MatchExact,
		}, nil
	}
	if len(exactMatches) > 1 {
		return nil, formatAmbiguousError(target, exactMatches)
	}

	// 2. Prefix match
	var prefixMatches []AliasMatch
	for _, ws := range resp.Workspaces {
		if strings.HasPrefix(strings.ToLower(ws.Alias), targetLower) {
			prefixMatches = append(prefixMatches, AliasMatch{
				WorkspaceID: ws.WorkspaceID,
				Alias:       ws.Alias,
				HumanName:   ws.HumanName,
				MatchType:   MatchPrefix,
			})
		}
	}

	if len(prefixMatches) == 1 {
		return &AliasResolution{
			WorkspaceID: prefixMatches[0].WorkspaceID,
			Alias:       prefixMatches[0].Alias,
			MatchType:   MatchPrefix,
		}, nil
	}
	if len(prefixMatches) > 1 {
		return nil, formatAmbiguousError(target, prefixMatches)
	}

	// 3. Substring match
	var substringMatches []AliasMatch
	for _, ws := range resp.Workspaces {
		if strings.Contains(strings.ToLower(ws.Alias), targetLower) {
			substringMatches = append(substringMatches, AliasMatch{
				WorkspaceID: ws.WorkspaceID,
				Alias:       ws.Alias,
				HumanName:   ws.HumanName,
				MatchType:   MatchSubstring,
			})
		}
	}

	if len(substringMatches) == 1 {
		return &AliasResolution{
			WorkspaceID: substringMatches[0].WorkspaceID,
			Alias:       substringMatches[0].Alias,
			MatchType:   MatchSubstring,
		}, nil
	}
	if len(substringMatches) > 1 {
		return nil, formatAmbiguousError(target, substringMatches)
	}

	// No matches - suggest similar aliases
	return nil, formatNotFoundError(target, resp.Workspaces)
}

// formatAmbiguousError creates an error message for ambiguous matches.
func formatAmbiguousError(target string, matches []AliasMatch) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ambiguous alias %q matches %d workspaces:\n", target, len(matches)))

	// Sort matches by alias for consistent output
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Alias < matches[j].Alias
	})

	for _, m := range matches {
		sb.WriteString(fmt.Sprintf("  %s (%s)\n", m.Alias, m.HumanName))
	}
	sb.WriteString("\nUse a more specific alias or the full workspace ID.")
	return fmt.Errorf("%s", sb.String())
}

// formatNotFoundError creates an error message with suggestions.
func formatNotFoundError(target string, workspaces []client.Workspace) error {
	suggestions := findSuggestions(target, workspaces)

	if len(suggestions) == 0 {
		return fmt.Errorf("no workspace found with alias %q", target)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("no workspace found with alias %q\n\nDid you mean:\n", target))
	for _, s := range suggestions {
		sb.WriteString(fmt.Sprintf("  %s (%s)\n", s.Alias, s.HumanName))
	}
	return fmt.Errorf("%s", sb.String())
}

// findSuggestions finds similar aliases for "did you mean?" suggestions.
// Uses edit distance (Levenshtein) to find the closest matches.
func findSuggestions(target string, workspaces []client.Workspace) []client.Workspace {
	type scored struct {
		ws    client.Workspace
		score int
	}

	targetLower := strings.ToLower(target)
	var candidates []scored

	for _, ws := range workspaces {
		aliasLower := strings.ToLower(ws.Alias)

		// Calculate edit distance
		dist := levenshteinDistance(targetLower, aliasLower)

		// Only suggest if reasonably close
		// Use the longer string length as base for threshold
		maxLen := len(target)
		if len(ws.Alias) > maxLen {
			maxLen = len(ws.Alias)
		}
		threshold := maxLen/2 + 3
		if dist <= threshold {
			candidates = append(candidates, scored{ws: ws, score: dist})
		}
	}

	// Sort by score (lower is better)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score < candidates[j].score
	})

	// Return top 3 suggestions
	result := make([]client.Workspace, 0, 3)
	for i := 0; i < len(candidates) && i < 3; i++ {
		result = append(result, candidates[i].ws)
	}

	return result
}

// levenshteinDistance calculates the edit distance between two strings.
func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create matrix
	matrix := make([][]int, len(a)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(b)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(a)][len(b)]
}
