package controller

import (
	"context"

	"github.com/docker/buildx/controller/control"
	"github.com/docker/buildx/controller/local"
	"github.com/docker/cli/cli/command"
)

func NewController(ctx context.Context, dockerCli command.Cli) control.BuildxController {
	return local.NewLocalBuildxController(ctx, dockerCli)
}
