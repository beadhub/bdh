package commands

import (
	"errors"
	"testing"
)

func TestShouldAutoInitRunConfig(t *testing.T) {
	missingConfigErr := errors.New("no .beadhub file found - run 'bdh :init' first")

	tests := []struct {
		name          string
		interactive   bool
		initialPrompt string
		basePrompt    string
		hasDispatcher bool
		cfgErr        error
		want          bool
	}{
		{
			name:        "true when interactive and missing workspace config with empty prompts",
			interactive: true,
			cfgErr:      missingConfigErr,
			want:        true,
		},
		{
			name:        "false when non interactive",
			interactive: false,
			cfgErr:      missingConfigErr,
			want:        false,
		},
		{
			name:          "false when initial prompt provided",
			interactive:   true,
			initialPrompt: "investigate issue",
			cfgErr:        missingConfigErr,
			want:          false,
		},
		{
			name:        "false when base prompt already configured",
			interactive: true,
			basePrompt:  "stay focused",
			cfgErr:      missingConfigErr,
			want:        false,
		},
		{
			name:          "false when dispatcher available",
			interactive:   true,
			hasDispatcher: true,
			cfgErr:        missingConfigErr,
			want:          false,
		},
		{
			name:        "false when config error is unrelated",
			interactive: true,
			cfgErr:      errors.New("invalid .beadhub config: workspace_id is required"),
			want:        false,
		},
		{
			name:        "false when no config error",
			interactive: true,
			cfgErr:      nil,
			want:        false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := shouldAutoInitRunConfig(tc.interactive, tc.initialPrompt, tc.basePrompt, tc.hasDispatcher, tc.cfgErr)
			if got != tc.want {
				t.Fatalf("shouldAutoInitRunConfig()=%v want %v", got, tc.want)
			}
		})
	}
}
