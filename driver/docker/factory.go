package docker

import (
	"context"

	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
)

const prioritySupported = 10
const priorityUnsupported = 99

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

func (*factory) Priority(cfg driver.InitConfig) int {
	if cfg.DockerAPI == nil {
		return priorityUnsupported
	}

	c, err := cfg.DockerAPI.DialHijack(context.TODO(), "/grpc", "h2c", nil)
	if err != nil {
		return priorityUnsupported
	}
	c.Close()

	return prioritySupported
}

func (*factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if cfg.DockerAPI == nil {
		return nil, errors.Errorf("docker driver requires docker API access")
	}

	v, err := cfg.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return nil, errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}

	return &Driver{InitConfig: cfg, version: v}, nil
}
