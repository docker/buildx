//go:build !go1.27

package policy

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeHTTPHostInvalidUTF8(t *testing.T) {
	// Unicode 15 encodes this invalid byte as U+FFFD, which IDNA then punycodes.
	host, err := normalizeHTTPHost(&url.URL{Scheme: "https", Host: "\x94.EXAMPLE.COM"})
	require.NoError(t, err)
	require.Equal(t, "xn--zn7c.example.com", host)
}
