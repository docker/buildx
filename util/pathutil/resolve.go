package pathutil

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// ExpandTilde expands tilde in paths (~/ or ~username/)
// Returns original path if expansion fails or path doesn't start with ~
func ExpandTilde(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	// Handle ~/path or just ~
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}

	// Handle ~username/path
	var username string
	var rest string

	if idx := strings.Index(path, "/"); idx > 1 {
		username = path[1:idx]
		rest = path[idx+1:]
	} else {
		username = path[1:]
	}

	u, err := user.Lookup(username)
	if err != nil {
		// If the user doesn't exist, return the unresolved path.
		// Matches shell behavior:
		// $ echo ~nonexistentuser/path
		// ~nonexistentuser/path
		return path
	}

	if rest == "" {
		return u.HomeDir
	}
	return filepath.Join(u.HomeDir, rest)
}

// ExpandTildePaths expands tilde in a slice of paths
func ExpandTildePaths(paths []string) []string {
	if paths == nil {
		return nil
	}
	expanded := make([]string, len(paths))
	for i, p := range paths {
		expanded[i] = ExpandTilde(p)
	}
	return expanded
}
