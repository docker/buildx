//go:build !windows
// +build !windows

package gitutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizePathUnix(t *testing.T) {
	assert.Equal(t, "/home/foobar", SanitizePath("/home/foobar"))
}

func TestSanitizePathWSL(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	assert.Equal(t, "/mnt/c/Users/foobar", SanitizePath("C:\\Users\\foobar"))
}
