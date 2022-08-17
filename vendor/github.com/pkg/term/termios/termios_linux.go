package termios

import (
	"golang.org/x/sys/unix"
)

const (
	IXON    = 0x00000400
	IXANY   = 0x00000800
	IXOFF   = 0x00001000
	CRTSCTS = 0x80000000
)

// Tcgetattr gets the current serial port settings.
func Tcgetattr(fd uintptr) (*unix.Termios, error) {
	return unix.IoctlGetTermios(int(fd), unix.TCGETS)
}

// Tcsetattr sets the current serial port settings.
func Tcsetattr(fd, action uintptr, argp *unix.Termios) error {
	var request uintptr
	switch action {
	case TCSANOW:
		request = unix.TCSETS
	case TCSADRAIN:
		request = unix.TCSETSW
	case TCSAFLUSH:
		request = unix.TCSETSF
	default:
		return unix.EINVAL
	}
	return unix.IoctlSetTermios(int(fd), uint(request), argp)
}

// Tcsendbreak transmits a continuous stream of zero-valued bits for a specific
// duration, if the terminal is using asynchronous serial data transmission. If
// duration is zero, it transmits zero-valued bits for at least 0.25 seconds, and not more that 0.5 seconds.
// If duration is not zero, it sends zero-valued bits for some
// implementation-defined length of time.
func Tcsendbreak(fd uintptr, duration int) error {
	return unix.IoctlSetInt(int(fd), unix.TCSBRKP, duration)
}

// Tcdrain waits until all output written to the object referred to by fd has been transmitted.
func Tcdrain(fd uintptr) error {
	// simulate drain with TCSADRAIN
	attr, err := Tcgetattr(fd)
	if err != nil {
		return err
	}
	return Tcsetattr(fd, TCSADRAIN, attr)
}

// Tcflush discards data written to the object referred to by fd but not transmitted, or data received but not read, depending on the value of selector.
func Tcflush(fd, selector uintptr) error {
	return unix.IoctlSetInt(int(fd), unix.TCFLSH, int(selector))
}

// Tiocinq returns the number of bytes in the input buffer.
func Tiocinq(fd uintptr) (int, error) {
	return unix.IoctlGetInt(int(fd), unix.TIOCINQ)
}

// Cfgetispeed returns the input baud rate stored in the termios structure.
func Cfgetispeed(attr *unix.Termios) uint32 { return attr.Ispeed }

// Cfgetospeed returns the output baud rate stored in the termios structure.
func Cfgetospeed(attr *unix.Termios) uint32 { return attr.Ospeed }
