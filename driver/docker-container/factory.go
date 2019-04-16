package docker

import (
	"context"

	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
)

const prioritySupported = 30
const priorityUnsupported = 70

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return "docker-container"
}

func (*factory) Usage() string {
	return "docker-container"
}

func (*factory) Priority(ctx context.Context, api dockerclient.APIClient) int {
	if api == nil {
		return priorityUnsupported
	}
	return prioritySupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if cfg.DockerAPI == nil {
		return nil, errors.Errorf("%s driver requires docker API access", f.Name())
	}

	v, err := cfg.DockerAPI.ServerVersion(ctx)
	if err != nil {
		return nil, errors.Wrapf(driver.ErrNotConnecting, err.Error())
	}

	return &Driver{factory: f, InitConfig: cfg, version: v}, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}
