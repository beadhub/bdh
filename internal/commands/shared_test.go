package commands

import "testing"

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected string
	}{
		{"less than minute", 45, "45s"},
		{"exactly one minute", 60, "1m"},
		{"minutes only", 300, "5m"},
		{"minutes with seconds", 185, "3m5s"},
		{"exactly one hour", 3600, "1h"},
		{"hours only", 7200, "2h"},
		{"hours with minutes", 3900, "1h5m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.seconds)
			if result != tt.expected {
				t.Errorf("formatDuration(%d) = %q, expected %q", tt.seconds, result, tt.expected)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid simple path", "src/api.py", false},
		{"valid glob", "src/*.py", false},
		{"valid directory", "tests/", false},
		{"empty path", "", true},
		{"path traversal", "../etc/passwd", true},
		{"path traversal in middle", "src/../../../etc/passwd", true},
		{"double dot only", "..", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
