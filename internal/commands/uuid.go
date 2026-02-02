package commands

import "regexp"

// uuidRegex matches UUID format (8-4-4-4-12 hex chars)
var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID checks if a string is a valid UUID format.
func isUUID(s string) bool {
	return uuidRegex.MatchString(s)
}
