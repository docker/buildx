package commands

import (
	"context"
	"fmt"

	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/util/walker"
)

type BreakpointsCmd struct {
	m types.Monitor
}

func NewBreakpointsCmd(m types.Monitor) types.Command {
	return &BreakpointsCmd{m}
}

func (cm *BreakpointsCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "breakpoints",
		HelpMessage: "lists registered breakpoints",
		HelpMessageLong: `
Usage:
  breakpoints
`,
	}
}

func (cm *BreakpointsCmd) Exec(ctx context.Context, args []string) error {
	cm.m.GetWalkerController().Breakpoints().ForEach(func(key string, bp walker.Breakpoint) bool {
		fmt.Printf("%s %s\n", key, bp.String())
		return true
	})
	return nil
}
