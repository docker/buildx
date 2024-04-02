package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/docker/cli/cli/streams"
)

func prompt(ctx context.Context, ins io.Reader, out io.Writer, msg string) (bool, error) {
	done := make(chan struct{})
	var ok bool
	go func() {
		ok = promptForConfirmation(ins, out, msg)
		close(done)
	}()
	select {
	case <-ctx.Done():
		return false, context.Cause(ctx)
	case <-done:
		return ok, nil
	}
}

// promptForConfirmation requests and checks confirmation from user.
// This will display the provided message followed by ' [y/N] '. If
// the user input 'y' or 'Y' it returns true other false.  If no
// message is provided "Are you sure you want to proceed? [y/N] "
// will be used instead.
//
// Copied from github.com/docker/cli since the upstream version changed
// recently with an incompatible change.
//
// See https://github.com/docker/buildx/pull/2359#discussion_r1544736494
// for discussion on the issue.
func promptForConfirmation(ins io.Reader, outs io.Writer, message string) bool {
	if message == "" {
		message = "Are you sure you want to proceed?"
	}
	message += " [y/N] "

	_, _ = fmt.Fprint(outs, message)

	// On Windows, force the use of the regular OS stdin stream.
	if runtime.GOOS == "windows" {
		ins = streams.NewIn(os.Stdin)
	}

	reader := bufio.NewReader(ins)
	answer, _, _ := reader.ReadLine()
	return strings.ToLower(string(answer)) == "y"
}
