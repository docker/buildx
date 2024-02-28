package commands

import (
	"context"
	"io"

	"github.com/docker/cli/cli/command"
)

func prompt(ctx context.Context, ins io.Reader, out io.Writer, msg string) (bool, error) {
	done := make(chan struct{})
	var ok bool
	go func() {
		ok = command.PromptForConfirmation(ins, out, msg)
		close(done)
	}()
	select {
	case <-ctx.Done():
		return false, context.Cause(ctx)
	case <-done:
		return ok, nil
	}
}
