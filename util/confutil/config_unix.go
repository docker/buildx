//go:build !windows
// +build !windows

package confutil

import (
	"os"
	"os/user"
	"strconv"
)

func init() {
	// If the SUDO_COMMAND environment variable is set, we are likely running
	// as a sudoer. In this case, we need to ensure that the user and group
	// IDs are set to the correct values only if sudo HOME env matches the home
	// directory of the user that ran sudo. This is necessary to ensure the
	// correct permissions are set on the files that are created in the
	// configuration directory.
	sudoerUID, sudoerGID = func() (int, int) {
		if _, ok := os.LookupEnv("SUDO_COMMAND"); !ok {
			return -1, -1
		}
		suidenv := os.Getenv("SUDO_UID") // https://www.sudo.ws/docs/man/sudo.man/#SUDO_UID
		sgidenv := os.Getenv("SUDO_GID") // https://www.sudo.ws/docs/man/sudo.man/#SUDO_GID
		if suidenv == "" || sgidenv == "" {
			return -1, -1
		}
		usr, err := user.LookupId(suidenv)
		if err != nil {
			return -1, -1
		}
		suid, err := strconv.Atoi(suidenv)
		if err != nil {
			return -1, -1
		}
		sgid, err := strconv.Atoi(sgidenv)
		if err != nil {
			return -1, -1
		}
		home, _ := os.UserHomeDir()
		if home == "" || usr.HomeDir != home {
			return -1, -1
		}
		return suid, sgid
	}()
}
