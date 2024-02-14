package gitutil

import (
	"os"
	"testing"

	"github.com/docker/buildx/util/osutil"
	"github.com/stretchr/testify/assert"
)

func TestSanitizePathWindows(t *testing.T) {
	expected := "C:\\Users\\foobar"
	if isGitBash() {
		expected = "C:/Users/foobar"
	}
	assert.Equal(t, expected, osutil.SanitizePath("C:/Users/foobar"))
}

func isGitBash() bool {
	// The MSYSTEM environment variable is used in MSYS2 environments,
	// including Git Bash, to select the active environment. This variable
	// dictates the environment in which the shell operates, influencing
	// factors like the path prefixes, default compilers, and system libraries
	// used: https://www.msys2.org/docs/environments/
	if _, ok := os.LookupEnv("MSYSTEM"); ok {
		return true
	}
	return false
}
