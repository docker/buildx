package docker

import (
	"context"

	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
)

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return "docker"
}

func (*factory) Usage() string {
	return "docker"
}

func (*factory) Priority() int {
	return 30
}

func (*factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if cfg.DockerAPI == nil {
		return nil, errors.Errorf("docker driver requires docker API access")
	}

	v, err := cfg.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return nil, errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}

	return &Driver{config: cfg, version: v}, nil
}
