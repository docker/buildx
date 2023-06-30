package commands

import (
	"context"
	"fmt"
	"io"

	controllererrors "github.com/docker/buildx/controller/errdefs"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/monitor/utils"
	"github.com/docker/buildx/util/progress"
	solverpb "github.com/moby/buildkit/solver/pb"
	"github.com/pkg/errors"
)

type AttachCmd struct {
	m types.Monitor

	stdout       io.WriteCloser
	progress     *progress.Printer
	invokeConfig controllerapi.InvokeConfig
}

func NewAttachCmd(m types.Monitor, stdout io.WriteCloser, progress *progress.Printer, invokeConfig controllerapi.InvokeConfig) types.Command {
	return &AttachCmd{m, stdout, progress, invokeConfig}
}

func (cm *AttachCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "attach",
		HelpMessage: "attach to a buildx server or a process in the container",
		HelpMessageLong: `
Usage:
  attach ID

ID is for a session (visible via list command) or a process (visible via ps command).
If you attached to a process, use Ctrl-a-c for switching the monitor to that process's STDIO.
`,
	}
}

func (cm *AttachCmd) Exec(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.Errorf("ID of session or process must be passed")
	}
	ref := args[1]
	var id string

	isProcess, err := utils.IsProcessID(ctx, cm.m, cm.m.AttachedSessionID(), ref)
	if err == nil && isProcess {
		cm.m.Attach(ctx, ref)
		id = ref
	}
	if id == "" {
		refs, err := cm.m.List(ctx)
		if err != nil {
			return errors.Errorf("failed to get the list of sessions: %v", err)
		}
		found := false
		for _, s := range refs {
			if s == ref {
				found = true
				break
			}
		}
		if !found {
			return errors.Errorf("unknown ID: %q", ref)
		}
		cm.m.Detach() // Finish existing attach
		cm.m.AttachSession(ref)
	}
	if !isProcess && id != "" {
		var walkerDef *solverpb.Definition
		if res, err := cm.m.Inspect(ctx, id); err == nil {
			walkerDef = res.Definition
			if !utils.IsSameDefinition(res.Definition, res.CurrentDefinition) && res.Options != nil {
				// Reload the current build if breakpoint debugger was ongoing on this session
				ref, _, err := cm.m.Build(ctx, *res.Options, nil, cm.progress)
				if err != nil {
					var be *controllererrors.BuildError
					if errors.As(err, &be) {
						ref = be.Ref
					} else {
						return errors.Errorf("failed to reload after attach: %v", err)
					}
				}
				st, err := cm.m.Inspect(ctx, ref)
				if err != nil {
					return err
				}
				walkerDef = st.Definition
				cm.m.AttachSession(ref)
				// rollback the running container with the new result
				id := cm.m.Rollback(ctx, cm.invokeConfig)
				fmt.Fprintf(cm.stdout, "Interactive container was restarted with process %q. Press Ctrl-a-c to switch to the new container", id)
			}
		}
		cm.m.RegisterWalkerController(utils.NewWalkerController(cm.m, cm.stdout, cm.invokeConfig, cm.progress, walkerDef))
	}
	fmt.Fprintf(cm.stdout, "Attached to process %q. Press Ctrl-a-c to switch to the new container\n", id)
	return nil
}
