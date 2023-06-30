package commands

import (
	"context"
	"fmt"

	"github.com/docker/buildx/monitor/types"
)

type ContinueCmd struct {
	m types.Monitor
}

func NewContinueCmd(m types.Monitor) types.Command {
	return &ContinueCmd{m}
}

func (cm *ContinueCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "continue",
		HelpMessage: "resumes the build until the next breakpoint",
		HelpMessageLong: `
Usage:
  continue
`,
	}
}

func (cm *ContinueCmd) Exec(ctx context.Context, args []string) error {
	wc := cm.m.GetWalkerController()
	wc.Continue()
	if (len(args) >= 2 && args[1] == "init") || !wc.IsStarted() {
		wc.WalkCancel() // Cancel current walking (needed especially for "init" option)
		if err := wc.StartWalk(); err != nil {
			fmt.Printf("failed to walk LLB: %v\n", err)
		}
	}
	return nil
}
