package commands

import (
	"context"
	"fmt"
	"io"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/buildx/monitor/types"
	"github.com/pkg/errors"
)

type AttachCmd struct {
	m types.Monitor

	stdout io.WriteCloser
}

func NewAttachCmd(m types.Monitor, stdout io.WriteCloser) types.Command {
	return &AttachCmd{m, stdout}
}

func (cm *AttachCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "attach",
		HelpMessage: "attach to a process in the container",
		HelpMessageLong: `
Usage:
  attach PID

PID is for a process (visible via ps command).
Use Ctrl-a-c for switching the monitor to that process's STDIO.
`,
	}
}

func (cm *AttachCmd) Exec(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.Errorf("PID of process must be passed")
	}
	pid := args[1]

	infos, err := cm.m.ListProcesses(ctx)
	if err != nil {
		return err
	}
	for _, p := range infos {
		if p.ProcessID == pid {
			cm.m.Attach(ctx, pid)
			fmt.Fprintf(cm.stdout, "Attached to process %q. Press Ctrl-a-c to switch to the new container\n", pid)
			return nil
		}
	}
	return errors.Wrapf(cerrdefs.ErrNotFound, "pid %s", pid)
}
