package commands

import (
	"context"
	"fmt"
	"io"

	cbuild "github.com/docker/buildx/controller/build"
	controllererrors "github.com/docker/buildx/controller/errdefs"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/pkg/errors"
)

type ReloadCmd struct {
	m types.Monitor

	stdout   io.WriteCloser
	progress *progress.Printer

	options      *cbuild.Options
	invokeConfig *controllerapi.InvokeConfig
}

func NewReloadCmd(m types.Monitor, stdout io.WriteCloser, progress *progress.Printer, options *cbuild.Options, invokeConfig *controllerapi.InvokeConfig) types.Command {
	return &ReloadCmd{m, stdout, progress, options, invokeConfig}
}

func (cm *ReloadCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "reload",
		HelpMessage: "reloads the context and build it",
		HelpMessageLong: `
Usage:
  reload
`,
	}
}

func (cm *ReloadCmd) Exec(ctx context.Context, args []string) error {
	bo := cm.m.Inspect(ctx)

	var resultUpdated bool
	cm.progress.Unpause()
	_, _, err := cm.m.Build(ctx, bo, nil, cm.progress) // TODO: support stdin, hold build ref
	cm.progress.Pause()
	if err != nil {
		var be *controllererrors.BuildError
		if errors.As(err, &be) {
			resultUpdated = true
		} else {
			fmt.Printf("failed to reload: %v\n", err)
		}
		// report error
		for _, s := range errdefs.Sources(err) {
			s.Print(cm.stdout)
		}
		fmt.Fprintf(cm.stdout, "ERROR: %v\n", err)
	} else {
		resultUpdated = true
	}
	if resultUpdated {
		// rollback the running container with the new result
		id := cm.m.Rollback(ctx, cm.invokeConfig)
		fmt.Fprintf(cm.stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)
	}
	return nil
}
