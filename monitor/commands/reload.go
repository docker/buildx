package commands

import (
	"context"

	"github.com/docker/buildx/monitor/types"
)

type ReloadCmd struct {
	m types.Monitor
}

func NewReloadCmd(m types.Monitor) types.Command {
	return &ReloadCmd{m: m}
}

func (cm *ReloadCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "reload",
		HelpMessage: "reloads the context and build it",
		HelpMessageLong: `
Usage:
  reload
`,
	}
}

func (cm *ReloadCmd) Exec(ctx context.Context, args []string) error {
	cm.m.Reload()
	return nil
}
