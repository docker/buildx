package termios

import (
	"bytes"

	"golang.org/x/sys/unix"
)

func open_pty_master() (uintptr, error) {
	return open_device("/dev/ptmx")
}

func Ptsname(fd uintptr) (string, error) {
	ptm, err := unix.IoctlGetPtmget(int(fd), unix.TIOCPTSNAME)
	if err != nil {
		return "", err
	}
	return string(ptm.Sn[:bytes.IndexByte(ptm.Sn[:], 0)]), nil
}

func grantpt(fd uintptr) error {
	return unix.IoctlSetInt(int(fd), unix.TIOCGRANTPT, 0)
}

func unlockpt(fd uintptr) error {
	return nil
}
