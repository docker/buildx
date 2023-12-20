package gitutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizePathWindows(t *testing.T) {
	assert.Equal(t, "C:\\Users\\foobar", sanitizePath("C:/Users/foobar"))
}
