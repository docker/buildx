//go:build !windows
// +build !windows

package confutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSubPath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		subPath  string
		expected bool
	}{
		{
			name:     "SubPath is a direct subdirectory",
			basePath: "/home/user",
			subPath:  "/home/user/docs",
			expected: true,
		},
		{
			name:     "SubPath is the same as basePath",
			basePath: "/home/user",
			subPath:  "/home/user",
			expected: false,
		},
		{
			name:     "SubPath is not a subdirectory",
			basePath: "/home/user",
			subPath:  "/home/otheruser",
			expected: false,
		},
		{
			name:     "SubPath is a nested subdirectory",
			basePath: "/home/user",
			subPath:  "/home/user/docs/reports",
			expected: true,
		},
		{
			name:     "SubPath is a sibling directory",
			basePath: "/home/user",
			subPath:  "/home/user2",
			expected: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ok, err := isSubPath(tt.basePath, tt.subPath)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, ok)
		})
	}
}
