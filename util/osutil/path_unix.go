//go:build !windows
// +build !windows

package osutil

// GetLongPathName is a no-op on non-Windows platforms.
func GetLongPathName(path string) (string, error) {
	return path, nil
}
