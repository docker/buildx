package commands

import (
	"context"
	"fmt"

	"github.com/docker/buildx/monitor/types"
	"github.com/docker/buildx/monitor/utils"
	"github.com/pkg/errors"
)

type DisconnectCmd struct {
	m types.Monitor
}

func NewDisconnectCmd(m types.Monitor) types.Command {
	return &DisconnectCmd{m}
}

func (cm *DisconnectCmd) Info() types.CommandInfo {
	return types.CommandInfo{
		Name:        "disconnect",
		HelpMessage: "disconnect a client from a buildx server. Specific session ID can be specified an arg",
		HelpMessageLong: fmt.Sprintf(`
Usage:
  disconnect [ID]

ID is for a session (visible via list command). Default is %q.
`, cm.m.AttachedSessionID()),
	}
}

func (cm *DisconnectCmd) Exec(ctx context.Context, args []string) error {
	target := cm.m.AttachedSessionID()
	if len(args) >= 2 {
		target = args[1]
	} else if target == "" {
		return errors.Errorf("no attaching session")
	}
	isProcess, err := utils.IsProcessID(ctx, cm.m, cm.m.AttachedSessionID(), target)
	if err == nil && isProcess {
		sid := cm.m.AttachedSessionID()
		if sid == "" {
			return errors.Errorf("no attaching session")
		}
		if err := cm.m.DisconnectProcess(ctx, sid, target); err != nil {
			return errors.Errorf("disconnecting from process failed %v", target)
		}
		return nil
	}
	if err := cm.m.DisconnectSession(ctx, target); err != nil {
		return errors.Errorf("disconnecting from session failed: %v", err)
	}
	return nil
}
