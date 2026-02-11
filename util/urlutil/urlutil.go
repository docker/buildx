package urlutil

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/dfgitutil"
)

// IsHTTPURL returns true if the provided str is an HTTP(S) URL by checking if
// it has a http:// or https:// scheme. No validation is performed to verify if
// the URL is well-formed.
func IsHTTPURL(str string) bool {
	return strings.HasPrefix(str, "https://") || strings.HasPrefix(str, "http://")
}

// IsRemoteURL returns true for HTTP(S) URLs and Git references.
func IsRemoteURL(c string) bool {
	if IsHTTPURL(c) {
		return true
	}
	if _, ok, _ := dfgitutil.ParseGitRef(c); ok {
		return true
	}
	return false
}
