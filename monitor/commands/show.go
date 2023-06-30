package commands

import (
	"context"
	"io"

	"github.com/docker/buildx/monitor/types"
	monitorutils "github.com/docker/buildx/monitor/utils"
	"github.com/pkg/errors"
)

type ShowCmd struct {
	m      types.Monitor
	stdout io.WriteCloser
}

func NewShowCmd(m types.Monitor, stdout io.WriteCloser) types.Command {
	return &ShowCmd{m, stdout}
}

func (cm *ShowCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "show",
		HelpMessage: "shows the debugging Dockerfile with breakpoint information",
		HelpMessageLong: `
Usage:
  show
`,
	}
}

func (cm *ShowCmd) Exec(ctx context.Context, args []string) error {
	st := cm.m.GetWalkerController().Inspect()
	if len(st.Definition.Source.Infos) != 1 {
		return errors.Errorf("list: multiple sources isn't supported")
	}
	monitorutils.PrintLines(cm.stdout, st.Definition.Source.Infos[0], st.Cursors, cm.m.GetWalkerController().Breakpoints(), 0, 0, true)
	return nil
}
