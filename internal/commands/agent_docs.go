package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Markers for bdh-injected content
const (
	bdhMarkerStart = "<!-- BEADHUB:START -->"
	bdhMarkerEnd   = "<!-- BEADHUB:END -->"
)

// Agent docs injection content (appended to existing files)
const bdhInstructionsContent = bdhMarkerStart + `
## BeadHub Coordination

This project uses ` + "`bdh`" + ` for multi-agent coordination. Run ` + "`bdh :policy`" + ` for instructions.

` + "```bash" + `
bdh :status    # your identity + team status
bdh :policy    # READ AND FOLLOW
bdh ready      # find work
` + "```" + `
` + bdhMarkerEnd

// Full AGENTS.md template for new files
const bdhAgentsTemplate = bdhMarkerStart + `
# Agent Instructions

This project uses ` + "`bdh`" + ` for multi-agent coordination and issue tracking.

## Quick Reference

` + "```bash" + `
bdh ready                              # Find available work
bdh show <id>                          # View issue details
bdh update <id> --status in_progress   # Claim work
bdh close <id>                         # Complete work
bdh sync --from-main                   # Sync with main branch
` + "```" + `

## Session Workflow

**Start every session:**
` + "```bash" + `
bdh :status    # your identity + team status
bdh :policy        # READ AND FOLLOW
bdh ready          # find work
` + "```" + `

**Before ending session:**
` + "```bash" + `
git status && git add <files>
bdh sync --from-main
git commit -m "..."
` + "```" + `

## Communication

- Default to mail (` + "`bdh :aweb mail send <alias> \"message\"`" + `) for async coordination
- Use chat (` + "`bdh :aweb chat`" + `) when blocked and need immediate response
- Respond immediately to WAITING notifications
` + bdhMarkerEnd

// Markers to detect existing instructions
var (
	// bd instructions present (need replacement) - case insensitive
	bdMarkerRegex = regexp.MustCompile(`(?i)\bbd\s+(ready|create|prime|sync|close|update|show|list)\b`)
)

// AgentDocsResult contains the result of injecting agent docs.
type AgentDocsResult struct {
	Created  []string // Files that were created from scratch
	Injected []string // Files that were modified (bdh section added)
	Skipped  []string // Files skipped (already has bdh instructions)
	Upgraded []string // Files that had bd instructions replaced with bdh
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

		// Remove existing bdh section if present (so we can update it)
		alreadyHadBdh := hasBdhInstructions(contentStr)
		if alreadyHadBdh {
			contentStr = removeBdhSection(contentStr)
		}

		// Check if bd instructions present (need replacement)
		hasBdInstructions := bdMarkerRegex.MatchString(contentStr)

		// Apply bd->bdh replacements
		newContent := contentStr
		for _, r := range bdToBdhReplacements {
			newContent = r.pattern.ReplaceAllString(newContent, r.replacement)
		}

		// If nothing changed and we already had bdh section, skip
		if newContent == contentStr && alreadyHadBdh && !hasBdInstructions {
			result.Skipped = append(result.Skipped, filename)
			continue
		}

		// Append bdh section
		if !strings.HasSuffix(newContent, "\n") {
			newContent += "\n"
		}
		newContent += "\n" + bdhInstructionsContent

		// Write back with original permissions
		if err := os.WriteFile(resolvedPath, []byte(newContent), fileMode); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: failed to write: %v", filename, err))
			continue
		}

		// Track result after successful write
		if hasBdInstructions {
			result.Upgraded = append(result.Upgraded, filename)
		} else {
			result.Injected = append(result.Injected, filename)
		}
	}

	// If no files were found or processed, create AGENTS.md from template
	if len(processedPaths) == 0 {
		agentsPath := filepath.Join(repoRoot, "AGENTS.md")
		if err := os.WriteFile(agentsPath, []byte(bdhAgentsTemplate), 0644); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("AGENTS.md: failed to create: %v", err))
		} else {
			result.Created = append(result.Created, "AGENTS.md")
		}
	}

	return result, nil
}

// hasBdhInstructions checks if content already has bdh instructions (using markers).
func hasBdhInstructions(content string) bool {
	return strings.Contains(content, bdhMarkerStart)
}

// removeBdhSection removes the existing bdh section (between markers) from content.
func removeBdhSection(content string) string {
	startIdx := strings.Index(content, bdhMarkerStart)
	if startIdx == -1 {
		return content
	}
	endIdx := strings.Index(content, bdhMarkerEnd)
	if endIdx == -1 {
		return content
	}
	endIdx += len(bdhMarkerEnd)

	// Remove the section and any trailing newlines
	before := content[:startIdx]
	after := content[endIdx:]

	// Trim trailing newlines from before and leading newlines from after
	before = strings.TrimRight(before, "\n")
	after = strings.TrimLeft(after, "\n")

	if before == "" {
		return after
	}
	if after == "" {
		return before
	}
	return before + "\n\n" + after
}

// PrintAgentDocsResult prints the result of agent docs injection.
func PrintAgentDocsResult(result *AgentDocsResult) {
	if len(result.Created) == 0 && len(result.Injected) == 0 && len(result.Upgraded) == 0 && len(result.Skipped) == 0 && len(result.Errors) == 0 {
		return
	}

	fmt.Println()
	fmt.Println("Agent instructions:")
	for _, f := range result.Created {
		fmt.Printf("  + Created %s with bdh instructions\n", f)
	}
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
bdh :status    # your identity + team status
bdh :policy        # READ AND FOLLOW
bdh ready          # find work
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
