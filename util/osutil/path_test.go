package osutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluateToExistingPath(t *testing.T) {
	tempDir, err := GetLongPathName(t.TempDir())
	require.NoError(t, err)

	existingFile := filepath.Join(tempDir, "existing_file")
	require.NoError(t, os.WriteFile(existingFile, []byte("test"), 0644))

	existingDir := filepath.Join(tempDir, "existing_dir")
	require.NoError(t, os.Mkdir(existingDir, 0755))

	symlinkToFile := filepath.Join(tempDir, "symlink_to_file")
	require.NoError(t, os.Symlink(existingFile, symlinkToFile))

	symlinkToDir := filepath.Join(tempDir, "symlink_to_dir")
	require.NoError(t, os.Symlink(existingDir, symlinkToDir))

	nonexistentPath := filepath.Join(tempDir, "nonexistent", "path", "file.txt")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Existing file",
			input:    existingFile,
			expected: existingFile,
		},
		{
			name:     "Existing directory",
			input:    existingDir,
			expected: existingDir,
		},
		{
			name:     "Symlink to file",
			input:    symlinkToFile,
			expected: existingFile,
		},
		{
			name:     "Symlink to directory",
			input:    symlinkToDir,
			expected: existingDir,
		},
		{
			name:     "Non-existent path",
			input:    nonexistentPath,
			expected: tempDir,
		},
		{
			name:     "Non-existent intermediate path",
			input:    filepath.Join(tempDir, "nonexistent", "file.txt"),
			expected: tempDir,
		},
		{
			name:  "Root path",
			input: "/",
			expected: func() string {
				root, _ := filepath.Abs("/")
				return root
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _, err := EvaluateToExistingPath(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}
