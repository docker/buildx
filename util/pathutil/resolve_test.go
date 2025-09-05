package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func expectedRootPath(subpath string) string {
	switch runtime.GOOS {
	case "windows":
		// Windows doesn't support ~username expansion
		return "~root/" + subpath
	case "darwin":
		return filepath.Join("/var/root", subpath)
	default:
		return filepath.Join("/root", subpath)
	}
}

func TestExpandTilde(t *testing.T) {
	// Get current user's home directory for testing
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no tilde",
			input:    "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "relative path no tilde",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "just tilde",
			input:    "~",
			expected: home,
		},
		{
			name:     "tilde with path",
			input:    "~/projects/test",
			expected: filepath.Join(home, "projects/test"),
		},
		{
			name:     "tilde with dotfile",
			input:    "~/.npmrc",
			expected: filepath.Join(home, ".npmrc"),
		},
		{
			name:     "invalid username",
			input:    "~nonexistentuser99999/path",
			expected: "~nonexistentuser99999/path", // Should return original
		},
		{
			name:     "root user home",
			input:    "~root/foo",
			expected: expectedRootPath("foo"),
		},
		{
			name:     "empty path",
			input:    "",
			expected: "",
		},
		{
			name:     "special prefixes not affected",
			input:    "docker-image://something",
			expected: "docker-image://something",
		},
		{
			name:     "git url not affected",
			input:    "git@github.com:user/repo.git",
			expected: "git@github.com:user/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandTilde(tt.input)
			if result != tt.expected {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExpandTildePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "mixed paths",
			input:    []string{"~/path1", "/absolute/path", "relative/path", "~/.ssh/id_rsa"},
			expected: []string{filepath.Join(home, "path1"), "/absolute/path", "relative/path", filepath.Join(home, ".ssh/id_rsa")},
		},
		{
			name:     "all tildes",
			input:    []string{"~/a", "~/b", "~/c"},
			expected: []string{filepath.Join(home, "a"), filepath.Join(home, "b"), filepath.Join(home, "c")},
		},
		{
			name:     "with invalid usernames",
			input:    []string{"~/valid", "~invaliduser/path", "~/another"},
			expected: []string{filepath.Join(home, "valid"), "~invaliduser/path", filepath.Join(home, "another")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandTildePaths(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("ExpandTildePaths(%v) returned %d items, want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("ExpandTildePaths[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}
