package commands

import (
	"context"

	"github.com/docker/buildx/monitor/types"
	"github.com/pkg/errors"
)

type KillCmd struct {
	m types.Monitor
}

func NewKillCmd(m types.Monitor) types.Command {
	return &KillCmd{m}
}

func (cm *KillCmd) Info() types.CommandInfo {
	return types.CommandInfo{HelpMessage: "kill buildx server"}
}

func (cm *KillCmd) Exec(ctx context.Context, args []string) error {
	if err := cm.m.Kill(ctx); err != nil {
		return errors.Errorf("failed to kill: %v", err)
	}
	return nil
}
