//go:build go1.27

package policy

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeHTTPHostInvalidUTF8(t *testing.T) {
	// Unicode 17 rejects the invalid byte instead of encoding it as U+FFFD.
	_, err := normalizeHTTPHost(&url.URL{Scheme: "https", Host: "\x94.EXAMPLE.COM"})
	require.ErrorContains(t, err, "invalid http host")
}
