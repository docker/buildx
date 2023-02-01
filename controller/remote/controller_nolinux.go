//go:build !linux

package remote

import (
	"context"
	"fmt"

	"github.com/docker/buildx/controller/control"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

func NewRemoteBuildxController(ctx context.Context, dockerCli command.Cli, opts control.ControlOptions) (control.BuildxController, error) {
	return nil, fmt.Errorf("remote buildx unsupported")
}

func AddControllerCommands(cmd *cobra.Command, dockerCli command.Cli) {}
