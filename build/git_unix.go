//go:build !windows
// +build !windows

package build

// getLongPathName is a no-op on non-Windows platforms.
func getLongPathName(path string) (string, error) {
	return path, nil
}
