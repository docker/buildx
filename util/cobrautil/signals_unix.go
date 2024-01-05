//go:build !windows

package cobrautil

import (
	"golang.org/x/sys/unix"
	"os"
)

var interruptSignals = []os.Signal{unix.SIGTERM, unix.SIGINT}
