package controller

import (
	"context"

	"github.com/docker/buildx/controller/control"
	"github.com/docker/buildx/controller/local"
	"github.com/docker/buildx/controller/remote"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewController(ctx context.Context, opts control.ControlOptions, dockerCli command.Cli) (c control.BuildxController, err error) {
	if !opts.Detach {
		logrus.Infof("launching local buildx controller")
		c = local.NewLocalBuildxController(ctx, dockerCli)
		return c, nil
	}

	logrus.Infof("connecting to buildx server")
	c, err = remote.NewRemoteBuildxController(ctx, dockerCli, opts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to use buildx server; use --detach=false")
	}
	return c, nil
}
