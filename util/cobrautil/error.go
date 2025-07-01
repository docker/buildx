package cobrautil

import (
	"fmt"
)

type ExitCodeError int

func (e ExitCodeError) Error() string {
	return fmt.Sprintf("exiting with code %d", int(e))
}

func (e ExitCodeError) Unwrap() error {
	return nil
}
