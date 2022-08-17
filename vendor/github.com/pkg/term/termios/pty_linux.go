package termios

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func open_pty_master() (uintptr, error) {
	return open_device("/dev/ptmx")
}

func Ptsname(fd uintptr) (string, error) {
	n, err := unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
	return fmt.Sprintf("/dev/pts/%d", n), err
}

func grantpt(fd uintptr) error {
	var n uintptr
	return ioctl(fd, unix.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
}

func unlockpt(fd uintptr) error {
	var n uintptr
	return ioctl(fd, unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&n)))
}
