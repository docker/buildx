package commands

import (
	"context"

	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/monitor/utils"
)

type ClearallCmd struct {
	m types.Monitor
}

func NewClearallCmd(m types.Monitor) types.Command {
	return &ClearallCmd{m}
}

func (cm *ClearallCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "clearall",
		HelpMessage: "clears all breakpoints",
		HelpMessageLong: `
Usage:
  clearall
`,
	}
}

func (cm *ClearallCmd) Exec(ctx context.Context, args []string) error {
	utils.SetDefaultBreakpoints(cm.m.GetWalkerController().Breakpoints())
	return nil
}
