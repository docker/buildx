package termios

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func posix_openpt(oflag int) (fd uintptr, err error) {
	// Copied from debian-golang-pty/pty_freebsd.go.
	r0, _, e1 := unix.Syscall(unix.SYS_POSIX_OPENPT, uintptr(oflag), 0, 0)
	fd = uintptr(r0)
	if e1 != 0 {
		err = e1
	}
	return
}

func open_pty_master() (uintptr, error) {
	return posix_openpt(unix.O_NOCTTY | unix.O_RDWR | unix.O_CLOEXEC)
}

func Ptsname(fd uintptr) (string, error) {
	n, err := unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
	return fmt.Sprintf("/dev/pts/%d", n), err
}

func grantpt(fd uintptr) error {
	return unix.IoctlSetInt(int(fd), unix.TIOCGPTN, 0)
}

func unlockpt(fd uintptr) error {
	return nil
}
