package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHookExists_Empty(t *testing.T) {
	settings := make(map[string]interface{})
	if hookExists(settings) {
		t.Error("Expected hookExists to return false for empty settings")
	}
}

func TestHookExists_NoHooks(t *testing.T) {
	settings := map[string]interface{}{
		"other": "value",
	}
	if hookExists(settings) {
		t.Error("Expected hookExists to return false when no hooks key")
	}
}

func TestHookExists_NoPostToolUse(t *testing.T) {
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{},
		},
	}
	if hookExists(settings) {
		t.Error("Expected hookExists to return false when no PostToolUse")
	}
}

func TestHookExists_OtherHooks(t *testing.T) {
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "some-other-command",
						},
					},
				},
			},
		},
	}
	if hookExists(settings) {
		t.Error("Expected hookExists to return false when bdh :notify not present")
	}
}

func TestHookExists_WithNotifyHook(t *testing.T) {
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "bdh :notify",
						},
					},
				},
			},
		},
	}
	if !hookExists(settings) {
		t.Error("Expected hookExists to return true when bdh :notify is present")
	}
}

func TestHookExists_DirectCommand(t *testing.T) {
	// Alternative simpler format
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "bdh :notify",
				},
			},
		},
	}
	if !hookExists(settings) {
		t.Error("Expected hookExists to return true for direct command format")
	}
}

func TestAddNotifyHook_Empty(t *testing.T) {
	settings := make(map[string]interface{})
	addNotifyHook(settings)

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected hooks to be added")
	}

	postToolUse, ok := hooks["PostToolUse"].([]interface{})
	if !ok {
		t.Fatal("Expected PostToolUse to be added")
	}

	if len(postToolUse) != 1 {
		t.Errorf("Expected 1 hook, got %d", len(postToolUse))
	}

	if !hookExists(settings) {
		t.Error("Hook should exist after adding")
	}
}

func TestAddNotifyHook_PreservesExisting(t *testing.T) {
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"command": "existing-command",
				},
			},
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"command": "pre-hook",
				},
			},
		},
		"other": "value",
	}

	addNotifyHook(settings)

	// Check existing values preserved
	if settings["other"] != "value" {
		t.Error("Other settings should be preserved")
	}

	hooks := settings["hooks"].(map[string]interface{})
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse should be preserved")
	}

	postToolUse := hooks["PostToolUse"].([]interface{})
	if len(postToolUse) != 2 {
		t.Errorf("Expected 2 hooks (1 existing + 1 new), got %d", len(postToolUse))
	}
}

func TestSetupClaudeHooks_CreatesNew(t *testing.T) {
	tmpDir := t.TempDir()

	result := SetupClaudeHooks(tmpDir, false)

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}
	if !result.Created {
		t.Error("Expected Created to be true")
	}
	if result.Updated || result.AlreadyExists || result.Skipped {
		t.Error("Only Created should be true")
	}

	// Verify file was created
	content, err := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Created file is not valid JSON: %v", err)
	}

	if !hookExists(settings) {
		t.Error("Hook should exist in created file")
	}
}

func TestSetupClaudeHooks_UpdatesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	// Create existing settings file
	existingSettings := map[string]interface{}{
		"other": "value",
	}
	content, _ := json.Marshal(existingSettings)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), content, 0644)

	result := SetupClaudeHooks(tmpDir, false)

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}
	if !result.Updated {
		t.Error("Expected Updated to be true")
	}
	if result.Created || result.AlreadyExists || result.Skipped {
		t.Error("Only Updated should be true")
	}

	// Verify existing content preserved
	newContent, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var settings map[string]interface{}
	json.Unmarshal(newContent, &settings)

	if settings["other"] != "value" {
		t.Error("Existing settings should be preserved")
	}
	if !hookExists(settings) {
		t.Error("Hook should exist after update")
	}
}

func TestSetupClaudeHooks_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	// Create settings with hook already present
	existingSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "bdh :notify",
						},
					},
				},
			},
		},
	}
	content, _ := json.Marshal(existingSettings)
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), content, 0644)

	result := SetupClaudeHooks(tmpDir, false)

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}
	if !result.AlreadyExists {
		t.Error("Expected AlreadyExists to be true")
	}
	if result.Created || result.Updated || result.Skipped {
		t.Error("Only AlreadyExists should be true")
	}
}
