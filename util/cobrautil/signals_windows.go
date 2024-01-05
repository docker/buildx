//go:build windows

package cobrautil

import (
	"os"
)

var interruptSignals = []os.Signal{os.Interrupt}
