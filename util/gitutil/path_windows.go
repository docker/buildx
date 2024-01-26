package gitutil

import (
	"os/exec"
	"path/filepath"
)

func gitPath(wd string) (string, error) {
	return exec.LookPath("git.exe")
}

func SanitizePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}
