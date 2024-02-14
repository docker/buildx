package gitutil

import (
	"os/exec"
)

func gitPath(wd string) (string, error) {
	return exec.LookPath("git.exe")
}
