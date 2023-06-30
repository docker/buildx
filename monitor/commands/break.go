package commands

import (
	"context"
	"strconv"

	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/util/walker"
	"github.com/pkg/errors"
)

type BreakCmd struct {
	m types.Monitor
}

func NewBreakCmd(m types.Monitor) types.Command {
	return &BreakCmd{m}
}

func (cm *BreakCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "break",
		HelpMessage: "sets a breakpoint",
		HelpMessageLong: `
Usage:
  break LINE

LINE is a line number to set a breakpoint.
`,
	}
}

func (cm *BreakCmd) Exec(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return errors.Errorf("break: specify line")
	}
	line, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return errors.Errorf("break: invalid line number: %q: %v", args[1], err)
	}
	cm.m.GetWalkerController().Breakpoints().Add("", walker.NewLineBreakpoint(line))
	return nil
}
