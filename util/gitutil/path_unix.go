//go:build !windows
// +build !windows

package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/moby/sys/mountinfo"
)

func gitPath(wd string) (string, error) {
	// On WSL2 we need to check if the current working directory is mounted on
	// a Windows drive and if so, we need to use the Windows git executable.
	if os.Getenv("WSL_DISTRO_NAME") != "" && wd != "" {
		// ensure any symlinks are resolved
		wdPath, err := filepath.EvalSymlinks(wd)
		if err != nil {
			return "", err
		}
		mi, err := mountinfo.GetMounts(mountinfo.ParentsFilter(wdPath))
		if err != nil {
			return "", err
		}
		// find the longest mount point
		var idx, maxlen int
		for i := range mi {
			if len(mi[i].Mountpoint) > maxlen {
				maxlen = len(mi[i].Mountpoint)
				idx = i
			}
		}
		if mi[idx].FSType == "9p" {
			if p, err := exec.LookPath("git.exe"); err == nil {
				return p, nil
			}
		}
	}
	return exec.LookPath("git")
}

var windowsPathRegex = regexp.MustCompile(`^[A-Za-z]:[\\/].*$`)

func sanitizePath(path string) string {
	// If we're running in WSL, we need to convert Windows paths to Unix paths.
	// This is because the git binary can be invoked through `git.exe` and
	// therefore returns Windows paths.
	if os.Getenv("WSL_DISTRO_NAME") != "" && windowsPathRegex.MatchString(path) {
		unixPath := strings.ReplaceAll(path, "\\", "/")
		drive := strings.ToLower(string(unixPath[0]))
		rest := filepath.Clean(unixPath[3:])
		return filepath.Join("/mnt", drive, rest)
	}
	return filepath.Clean(path)
}
