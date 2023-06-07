package commands

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/docker/buildx/monitor/types"
	"github.com/pkg/errors"
)

type PsCmd struct {
	m      types.Monitor
	stdout io.WriteCloser
}

func NewPsCmd(m types.Monitor, stdout io.WriteCloser) types.Command {
	return &PsCmd{m, stdout}
}

func (cm *PsCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "ps",
		HelpMessage: `list processes invoked by "exec". Use "attach" to attach IO to that process`,
		HelpMessageLong: `
Usage:
  ps
`,
	}
}

func (cm *PsCmd) Exec(ctx context.Context, args []string) error {
	ref := cm.m.AttachedSessionID()
	if ref == "" {
		return errors.Errorf("no attaching session")
	}
	plist, err := cm.m.ListProcesses(ctx, ref)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(cm.stdout, 1, 8, 1, '\t', 0)
	fmt.Fprintln(tw, "PID\tCURRENT_SESSION\tCOMMAND")
	for _, p := range plist {
		fmt.Fprintf(tw, "%-20s\t%v\t%v\n", p.ProcessID, p.ProcessID == cm.m.AttachedPID(), append(p.InvokeConfig.Entrypoint, p.InvokeConfig.Cmd...))
	}
	tw.Flush()
	return nil
}
