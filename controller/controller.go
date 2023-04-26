package controller

import (
	"context"
	"fmt"

	"github.com/docker/buildx/controller/control"
	"github.com/docker/buildx/controller/local"
	"github.com/docker/buildx/controller/remote"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
)

func NewController(ctx context.Context, opts control.ControlOptions, dockerCli command.Cli, pw progress.Writer) (control.BuildxController, error) {
	var name string
	if opts.Detach {
		name = "remote"
	} else {
		name = "local"
	}

	var c control.BuildxController
	err := progress.Wrap(fmt.Sprintf("[internal] connecting to %s controller", name), pw.Write, func(l progress.SubLogger) (err error) {
		if opts.Detach {
			c, err = remote.NewRemoteBuildxController(ctx, dockerCli, opts, l)
		} else {
			c = local.NewLocalBuildxController(ctx, dockerCli, l)
		}
		return err
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to start buildx controller")
	}
	return c, nil
}
