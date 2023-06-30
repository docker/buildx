package commands

import (
	"context"

	"github.com/docker/buildx/monitor/types"
	"github.com/pkg/errors"
)

type NextCmd struct {
	m types.Monitor
}

func NewNextCmd(m types.Monitor) types.Command {
	return &NextCmd{m}
}

func (cm *NextCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "next",
		HelpMessage: "resumes the build until the next vertex",
		HelpMessageLong: `
Usage:
  next
`,
	}
}

func (cm *NextCmd) Exec(ctx context.Context, args []string) error {
	if err := cm.m.GetWalkerController().Next(); err != nil {
		return errors.Errorf("next: %s : If walker isn't runnig, might need to run \"continue\" command first", err)
	}
	return nil
}
