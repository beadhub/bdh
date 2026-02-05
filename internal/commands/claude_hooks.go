package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeHooksResult contains the result of setting up Claude Code hooks.
type ClaudeHooksResult struct {
	Created       bool   // .claude/settings.json was created
	Updated       bool   // Hook was added to existing settings
	AlreadyExists bool   // Hook was already configured
	Skipped       bool   // User declined or error occurred
	Error         error  // Any error that occurred
	FilePath      string // Path to the settings file
}

// notifyHookCommand is the command to run for chat notifications.
const notifyHookCommand = "bdh :notify"

// SetupClaudeHooks configures the PostToolUse hook in .claude/settings.json.
// If askConfirmation is true (TTY mode), prompts before modifying.
func SetupClaudeHooks(repoRoot string, askConfirmation bool) *ClaudeHooksResult {
	result := &ClaudeHooksResult{}

	claudeDir := filepath.Join(repoRoot, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")
	result.FilePath = settingsPath

	// Check if settings file exists
	existingContent, err := os.ReadFile(settingsPath)
	if err != nil && !os.IsNotExist(err) {
		result.Error = fmt.Errorf("reading %s: %w", settingsPath, err)
		result.Skipped = true
		return result
	}

	var settings map[string]interface{}
	if len(existingContent) > 0 {
		if err := json.Unmarshal(existingContent, &settings); err != nil {
			result.Error = fmt.Errorf("parsing %s: %w", settingsPath, err)
			result.Skipped = true
			return result
		}
	} else {
		settings = make(map[string]interface{})
	}

	// Check if hook already exists
	if hookExists(settings) {
		result.AlreadyExists = true
		return result
	}

	// Ask for confirmation in TTY mode
	if askConfirmation && isTTY() {
		fmt.Printf("\nSet up Claude Code hook for chat notifications? (y/n): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "yes" && response != "Y" {
			result.Skipped = true
			return result
		}
	}

	// Add the hook
	addNotifyHook(settings)

	// Create .claude directory if needed
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		result.Error = fmt.Errorf("creating %s: %w", claudeDir, err)
		result.Skipped = true
		return result
	}

	// Write settings
	newContent, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		result.Error = fmt.Errorf("marshaling settings: %w", err)
		result.Skipped = true
		return result
	}

	if err := os.WriteFile(settingsPath, append(newContent, '\n'), 0644); err != nil {
		result.Error = fmt.Errorf("writing %s: %w", settingsPath, err)
		result.Skipped = true
		return result
	}

	if len(existingContent) > 0 {
		result.Updated = true
	} else {
		result.Created = true
	}

	return result
}

// hookExists checks if the bdh :notify hook is already configured.
func hookExists(settings map[string]interface{}) bool {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	postToolUse, ok := hooks["PostToolUse"].([]interface{})
	if !ok {
		return false
	}

	for _, entry := range postToolUse {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}

		// Check for matcher-based hook structure
		innerHooks, ok := entryMap["hooks"].([]interface{})
		if ok {
			for _, h := range innerHooks {
				hookMap, ok := h.(map[string]interface{})
				if !ok {
					continue
				}
				if cmd, ok := hookMap["command"].(string); ok && cmd == notifyHookCommand {
					return true
				}
			}
		}

		// Also check direct command structure (simpler format)
		if cmd, ok := entryMap["command"].(string); ok && cmd == notifyHookCommand {
			return true
		}
	}

	return false
}

// addNotifyHook adds the bdh :notify hook to the settings.
func addNotifyHook(settings map[string]interface{}) {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	postToolUse, ok := hooks["PostToolUse"].([]interface{})
	if !ok {
		postToolUse = make([]interface{}, 0)
	}

	// Add the hook with matcher for all tools
	newHook := map[string]interface{}{
		"matcher": ".*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": notifyHookCommand,
			},
		},
	}

	postToolUse = append(postToolUse, newHook)
	hooks["PostToolUse"] = postToolUse
}

// PrintClaudeHooksResult prints the result of setting up Claude Code hooks.
func PrintClaudeHooksResult(result *ClaudeHooksResult) {
	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not set up Claude Code hooks: %v\n", result.Error)
		printManualHookInstructions()
		return
	}

	if result.AlreadyExists {
		fmt.Println("Claude Code hook: already configured")
		return
	}

	if result.Skipped {
		fmt.Println("Claude Code hook: skipped")
		printManualHookInstructions()
		return
	}

	if result.Created {
		fmt.Printf("Created %s with notification hook\n", result.FilePath)
	} else if result.Updated {
		fmt.Printf("Added notification hook to %s\n", result.FilePath)
	}

	fmt.Println("  Agents will be notified of pending chats after each tool call")
}

// printManualHookInstructions prints manual setup instructions.
func printManualHookInstructions() {
	fmt.Println()
	fmt.Println("To enable chat notifications, add to .claude/settings.json:")
	fmt.Println(`  {
    "hooks": {
      "PostToolUse": [{
        "matcher": ".*",
        "hooks": [{"type": "command", "command": "bdh :notify"}]
      }]
    }
  }`)
}
