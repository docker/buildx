package commands

import (
	"context"
	"fmt"
	"io"

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

	options      *controllerapi.BuildOptions
	invokeConfig *controllerapi.InvokeConfig
}

func NewReloadCmd(m types.Monitor, stdout io.WriteCloser, progress *progress.Printer, options *controllerapi.BuildOptions, invokeConfig *controllerapi.InvokeConfig) types.Command {
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
	var bo *controllerapi.BuildOptions
	if ref := cm.m.AttachedSessionID(); ref != "" {
		// Rebuilding an existing session; Restore the build option used for building this session.
		res, err := cm.m.Inspect(ctx, ref)
		if err != nil {
			fmt.Printf("failed to inspect the current build session: %v\n", err)
		} else {
			bo = res.Options
		}
	} else {
		bo = cm.options
	}
	if bo == nil {
		return errors.Errorf("no build option is provided")
	}
	if ref := cm.m.AttachedSessionID(); ref != "" {
		if err := cm.m.Disconnect(ctx, ref); err != nil {
			fmt.Println("disconnect error", err)
		}
	}
	var resultUpdated bool
	cm.progress.Unpause()
	ref, _, _, err := cm.m.Build(ctx, bo, nil, cm.progress) // TODO: support stdin, hold build ref
	cm.progress.Pause()
	if err != nil {
		var be *controllererrors.BuildError
		if errors.As(err, &be) {
			ref = be.Ref
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
	cm.m.AttachSession(ref)
	if resultUpdated {
		// rollback the running container with the new result
		id := cm.m.Rollback(ctx, cm.invokeConfig)
		fmt.Fprintf(cm.stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container\n", id)
	}
	return nil
}
