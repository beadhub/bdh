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

// Edge case tests recommended by code review

func TestHookExists_NotifyBuriedInMultipleCommands(t *testing.T) {
	// bdh :notify is the 2nd of 3 commands in a nested hooks array
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "first-command",
						},
						map[string]interface{}{
							"type":    "command",
							"command": "bdh :notify",
						},
						map[string]interface{}{
							"type":    "command",
							"command": "third-command",
						},
					},
				},
			},
		},
	}
	if !hookExists(settings) {
		t.Error("Expected hookExists to find bdh :notify buried in multiple commands")
	}
}

func TestHookExists_MixedFormats(t *testing.T) {
	// Mix of direct and nested formats in same PostToolUse array
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				// Direct format
				map[string]interface{}{
					"type":    "command",
					"command": "direct-hook",
				},
				// Nested format with bdh :notify
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "bdh :notify",
						},
					},
				},
				// Another direct format
				map[string]interface{}{
					"command": "another-direct",
				},
			},
		},
	}
	if !hookExists(settings) {
		t.Error("Expected hookExists to find bdh :notify in mixed format array")
	}
}

func TestHookExists_MultiplePostToolUseEntries(t *testing.T) {
	// Multiple PostToolUse entries, notify is in the 3rd one
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"command": "bash-hook"},
					},
				},
				map[string]interface{}{
					"matcher": "Edit",
					"hooks": []interface{}{
						map[string]interface{}{"command": "edit-hook"},
					},
				},
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{"command": "bdh :notify"},
					},
				},
			},
		},
	}
	if !hookExists(settings) {
		t.Error("Expected hookExists to find bdh :notify in 3rd PostToolUse entry")
	}
}

func TestAddNotifyHook_PreservesMultipleExisting(t *testing.T) {
	// 3 existing PostToolUse hooks
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{"command": "hook1"},
				map[string]interface{}{"command": "hook2"},
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{"command": "hook3"},
					},
				},
			},
		},
	}

	addNotifyHook(settings)

	hooks := settings["hooks"].(map[string]interface{})
	postToolUse := hooks["PostToolUse"].([]interface{})

	if len(postToolUse) != 4 {
		t.Errorf("Expected 4 hooks (3 existing + 1 new), got %d", len(postToolUse))
	}

	// Verify all original hooks still there
	hook1 := postToolUse[0].(map[string]interface{})
	if hook1["command"] != "hook1" {
		t.Error("First hook should be preserved")
	}

	hook2 := postToolUse[1].(map[string]interface{})
	if hook2["command"] != "hook2" {
		t.Error("Second hook should be preserved")
	}

	hook3 := postToolUse[2].(map[string]interface{})
	if hook3["matcher"] != "Bash" {
		t.Error("Third hook (nested) should be preserved")
	}

	// Verify new hook was added
	if !hookExists(settings) {
		t.Error("bdh :notify hook should exist after adding")
	}
}

func TestSetupClaudeHooks_RoundTrip(t *testing.T) {
	// Create, write, read back, verify hookExists still works
	tmpDir := t.TempDir()

	// First call creates the file
	result1 := SetupClaudeHooks(tmpDir, false)
	if result1.Error != nil {
		t.Fatalf("First setup failed: %v", result1.Error)
	}
	if !result1.Created {
		t.Error("Expected file to be created")
	}

	// Read back and verify
	content, err := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	if !hookExists(settings) {
		t.Error("hookExists should return true for file we just created")
	}

	// Second call should detect it already exists
	result2 := SetupClaudeHooks(tmpDir, false)
	if result2.Error != nil {
		t.Fatalf("Second setup failed: %v", result2.Error)
	}
	if !result2.AlreadyExists {
		t.Error("Expected AlreadyExists on second call")
	}
}

func TestSetupClaudeHooks_PreservesExistingHooks(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, ".claude")
	os.MkdirAll(claudeDir, 0755)

	// Create settings with existing PostToolUse hooks
	existingSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PostToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "existing-bash-hook",
						},
					},
				},
			},
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"command": "pre-hook",
				},
			},
		},
		"permissions": map[string]interface{}{
			"allow": []string{"Bash"},
		},
	}
	content, _ := json.MarshalIndent(existingSettings, "", "  ")
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), content, 0644)

	result := SetupClaudeHooks(tmpDir, false)

	if result.Error != nil {
		t.Fatalf("Unexpected error: %v", result.Error)
	}
	if !result.Updated {
		t.Error("Expected Updated to be true")
	}

	// Read back and verify everything preserved
	newContent, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var settings map[string]interface{}
	json.Unmarshal(newContent, &settings)

	// Check permissions preserved
	perms, ok := settings["permissions"].(map[string]interface{})
	if !ok {
		t.Error("permissions should be preserved")
	} else {
		allow, ok := perms["allow"].([]interface{})
		if !ok || len(allow) != 1 {
			t.Error("permissions.allow should be preserved")
		}
	}

	// Check PreToolUse preserved
	hooks := settings["hooks"].(map[string]interface{})
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("PreToolUse should be preserved")
	}

	// Check existing PostToolUse hook preserved
	postToolUse := hooks["PostToolUse"].([]interface{})
	if len(postToolUse) != 2 {
		t.Errorf("Expected 2 PostToolUse hooks, got %d", len(postToolUse))
	}

	// First should be the existing Bash hook
	firstHook := postToolUse[0].(map[string]interface{})
	if firstHook["matcher"] != "Bash" {
		t.Error("Existing Bash hook should be first")
	}

	// bdh :notify should now exist
	if !hookExists(settings) {
		t.Error("bdh :notify should exist after update")
	}
}
