package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Agent docs injection content
const bdhInstructionsContent = `## BeadHub Coordination

This project uses ` + "`bdh`" + ` for multi-agent coordination. Run ` + "`bdh :policy`" + ` for instructions.

` + "```bash" + `
bdh :status    # your identity
bdh :policy    # READ AND FOLLOW
bdh ready      # find work
` + "```" + `
`

const bdhUpgradeNotice = `> **IMPORTANT**: This project uses ` + "`bdh`" + ` (BeadHub) for coordination. Always use ` + "`bdh`" + ` commands instead of ` + "`bd`" + `. The ` + "`bdh`" + ` wrapper coordinates work across agents and syncs with the BeadHub server.

`

// Markers to detect existing instructions
var (
	// bdh instructions already present - check for the injected section header
	bdhMarkers = []string{
		"## BeadHub Coordination",
	}

	// bd instructions present (need upgrade notice) - case insensitive
	bdMarkerRegex = regexp.MustCompile(`(?i)\bbd\s+(ready|create|prime|sync|close|update|show|list)\b`)
)

// AgentDocsResult contains the result of injecting agent docs.
type AgentDocsResult struct {
	Injected []string // Files that were modified
	Skipped  []string // Files skipped (already has bdh instructions)
	Upgraded []string // Files that had bd instructions and got upgrade notice
	Errors   []string // Files that had errors
}

// InjectAgentDocs injects bdh instructions into CLAUDE.md and AGENTS.md files.
// It handles symlinks by resolving them and avoiding duplicate writes.
func InjectAgentDocs(repoRoot string) (*AgentDocsResult, error) {
	result := &AgentDocsResult{}

	// Files to check
	candidates := []string{"CLAUDE.md", "AGENTS.md"}

	// Track resolved paths to avoid writing same file twice
	processedPaths := make(map[string]bool)

	for _, filename := range candidates {
		filePath := filepath.Join(repoRoot, filename)

		// Check if file exists
		info, err := os.Lstat(filePath)
		if os.IsNotExist(err) {
			// File doesn't exist - skip silently
			continue
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filename, err))
			continue
		}

		// Resolve symlink if applicable
		resolvedPath := filePath
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(filePath)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: failed to resolve symlink: %v", filename, err))
				continue
			}
			resolvedPath = resolved
		}

		// Check if we already processed this resolved path
		if processedPaths[resolvedPath] {
			// Already processed via another symlink
			continue
		}
		processedPaths[resolvedPath] = true

		// Get file info for permissions
		fileInfo, err := os.Stat(resolvedPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: failed to stat: %v", filename, err))
			continue
		}
		fileMode := fileInfo.Mode().Perm()

		// Read file content
		content, err := os.ReadFile(resolvedPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: failed to read: %v", filename, err))
			continue
		}

		contentStr := string(content)

		// Check if bdh instructions already present
		if hasBdhInstructions(contentStr) {
			result.Skipped = append(result.Skipped, filename)
			continue
		}

		// Check if bd instructions present (need upgrade notice)
		needsUpgradeNotice := bdMarkerRegex.MatchString(contentStr)

		// Prepare content to inject
		var injection string
		if needsUpgradeNotice {
			injection = bdhUpgradeNotice + bdhInstructionsContent
		} else {
			injection = bdhInstructionsContent
		}

		// Append to file
		newContent := contentStr
		if !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		newContent += "\n" + injection

		// Write back with original permissions
		if err := os.WriteFile(resolvedPath, []byte(newContent), fileMode); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: failed to write: %v", filename, err))
			continue
		}

		// Track result after successful write
		if needsUpgradeNotice {
			result.Upgraded = append(result.Upgraded, filename)
		} else {
			result.Injected = append(result.Injected, filename)
		}
	}

	return result, nil
}

// hasBdhInstructions checks if content already has bdh instructions.
func hasBdhInstructions(content string) bool {
	for _, marker := range bdhMarkers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

// PrintAgentDocsResult prints the result of agent docs injection.
func PrintAgentDocsResult(result *AgentDocsResult) {
	if len(result.Injected) == 0 && len(result.Upgraded) == 0 && len(result.Skipped) == 0 && len(result.Errors) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("Agent instructions:")
	for _, f := range result.Injected {
		fmt.Printf("  + Injected bdh instructions into %s\n", f)
	}
	for _, f := range result.Upgraded {
		fmt.Printf("  + Injected bdh instructions into %s (upgrade notice added)\n", f)
	}
	for _, f := range result.Skipped {
		fmt.Printf("  - Skipped %s (bdh instructions already present)\n", f)
	}
	for _, e := range result.Errors {
		fmt.Printf("  ! Error: %s\n", e)
	}
}

// primeHeader is prepended to the PRIME.md to explain the bdh override.
const primeHeader = `# BeadHub Workspace

> Always use ` + "`bdh`" + ` (not ` + "`bd`" + `) — it coordinates work across agents.

**Start every session:**
` + "```bash" + `
bdh :status    # your identity
bdh :policy    # READ AND FOLLOW
bdh ready      # find work
` + "```" + `

**Before ending session:**
` + "```bash" + `
git status && git add <files>
bdh sync --from-main
git commit -m "..."
` + "```" + `

---

`

// primeFooter is empty - all instructions are in :policy now
const primeFooter = ``

// bdToBdhReplacements defines patterns to replace bd with bdh.
var bdToBdhReplacements = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	// Command references in backticks: `bd foo` -> `bdh foo`
	{regexp.MustCompile("`bd\\s"), "`bdh "},
	{regexp.MustCompile("`bd`"), "`bdh`"},
	// Bare commands at start of line or after whitespace
	{regexp.MustCompile(`(^|\s)bd\s+(ready|create|close|update|show|list|sync|dep|blocked|stats|doctor|prime|export)`), "${1}bdh ${2}"},
	// Run `bd ...` patterns
	{regexp.MustCompile("Run `bd\\s"), "Run `bdh "},
	{regexp.MustCompile("run `bd\\s"), "run `bdh "},
	// "prefer bd" patterns (bd followed by em-dash, comma, period, or end of word boundary)
	{regexp.MustCompile(`prefer bd([—,.]|\b)`), "prefer bdh${1}"},
}

// GetBeadsPrimeContent runs `bd prime --export` and returns the content with bd replaced by bdh.
func GetBeadsPrimeContent() (string, error) {
	cmd := exec.Command("bd", "prime", "--export")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run 'bd prime --export': %w", err)
	}

	content := string(output)
	for _, r := range bdToBdhReplacements {
		content = r.pattern.ReplaceAllString(content, r.replacement)
	}
	return content, nil
}

// PrimeOverrideResult contains the result of injecting the PRIME.md override.
type PrimeOverrideResult struct {
	Injected bool   // True if PRIME.md was created/updated
	Skipped  bool   // True if already has bdh content
	Error    string // Error message if failed
}

// InjectPrimeOverride creates .beads/PRIME.md with bdh-aware content.
// It runs `bd prime --export` to get the default content, then replaces
// bd references with bdh and adds bdh-specific sections.
func InjectPrimeOverride(repoRoot string) *PrimeOverrideResult {
	result := &PrimeOverrideResult{}

	beadsDir := filepath.Join(repoRoot, ".beads")
	primePath := filepath.Join(beadsDir, "PRIME.md")

	// Check if .beads directory exists
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		// No .beads directory - skip silently (beads not initialized)
		return result
	}

	// Check if PRIME.md already exists with bdh content
	if existing, err := os.ReadFile(primePath); err == nil {
		if strings.Contains(string(existing), "BeadHub") || strings.Contains(string(existing), "bdh") {
			result.Skipped = true
			return result
		}
	}

	// Get beads prime content with bd->bdh replacements
	content, err := GetBeadsPrimeContent()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	// Build final content: header + modified bd content + footer
	finalContent := primeHeader + content + primeFooter

	// Write to PRIME.md
	if err := os.WriteFile(primePath, []byte(finalContent), 0644); err != nil {
		result.Error = fmt.Sprintf("failed to write PRIME.md: %v", err)
		return result
	}

	result.Injected = true
	return result
}

// PrintPrimeOverrideResult prints the result of PRIME.md injection.
func PrintPrimeOverrideResult(result *PrimeOverrideResult) {
	if result.Injected {
		fmt.Println("  + Created .beads/PRIME.md with bdh instructions")
	}
	if result.Skipped {
		fmt.Println("  - Skipped .beads/PRIME.md (bdh content already present)")
	}
	if result.Error != "" {
		fmt.Printf("  ! Error: %s\n", result.Error)
	}
}
