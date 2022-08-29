//go:build !linux

package commands

import (
	"context"
	"fmt"

	"github.com/docker/buildx/monitor"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

func newRemoteBuildxController(ctx context.Context, dockerCli command.Cli, opts buildOptions) (monitor.BuildxController, error) {
	return nil, fmt.Errorf("remote buildx unsupported")
}

func addControllerCommands(cmd *cobra.Command, dockerCli command.Cli, rootOpts *rootOptions) {}
