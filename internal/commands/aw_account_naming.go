package commands

import "strings"

func sanitizeAccountKeyComponent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "x"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func deriveAccountName(serverName, projectSlug, alias string) string {
	return "acct-" + sanitizeAccountKeyComponent(serverName) + "__" + sanitizeAccountKeyComponent(projectSlug) + "__" + sanitizeAccountKeyComponent(alias)
}
