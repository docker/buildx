package commands

import (
	"context"

	"github.com/docker/buildx/monitor/types"
	"github.com/pkg/errors"
)

type ClearCmd struct {
	m types.Monitor
}

func NewClearCmd(m types.Monitor) types.Command {
	return &ClearCmd{m}
}

func (cm *ClearCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "clear",
		HelpMessage: "clears a breakpoint",
		HelpMessageLong: `
Usage:
  clear KEY

KEY is the name of the breakpoint.
Use "breakpoints" command to list keys of the breakpoints.
`,
	}
}

func (cm *ClearCmd) Exec(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.Errorf("clear: specify breakpoint key")
	}
	cm.m.GetWalkerController().Breakpoints().Clear(args[1])
	return nil
}
