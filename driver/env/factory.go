package env

import (
	"context"

	"github.com/docker/buildx/driver"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
)

const prioritySupported = 50
const priorityUnsupported = 90

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return "env"
}

func (*factory) Usage() string {
	return "env"
}

func (*factory) Priority(ctx context.Context, api dockerclient.APIClient) int {
	if api == nil {
		return priorityUnsupported
	}

	return prioritySupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if len(cfg.Files) > 0 {
		return nil, errors.Errorf("setting config file is not supported for remote driver")
	}

	return &Driver{
		factory:      f,
		InitConfig:   cfg,
		BuldkitdAddr: cfg.BuldkitdAddr,
		BuildkitAPI:  cfg.BuildkitAPI,
	}, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}
