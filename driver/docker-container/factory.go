package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/buildx/driver"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
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

func (*factory) Priority(ctx context.Context, endpoint string, api dockerclient.APIClient, dialMeta map[string][]string) int {
	if api == nil {
		return priorityUnsupported
	}
	return prioritySupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if cfg.DockerAPI == nil {
		return nil, errors.Errorf("%s driver requires docker API access", f.Name())
	}
	d := &Driver{factory: f, InitConfig: cfg}
	for k, v := range cfg.DriverOpts {
		switch {
		case k == "network":
			d.netMode = v
			if v == "host" {
				d.InitConfig.BuildkitFlags = append(d.InitConfig.BuildkitFlags, "--allow-insecure-entitlement=network.host")
			}
		case k == "image":
			d.image = v
		case k == "cgroup-parent":
			d.cgroupParent = v
		case strings.HasPrefix(k, "env."):
			envName := strings.TrimPrefix(k, "env.")
			if envName == "" {
				return nil, errors.Errorf("invalid env option %q, expecting env.FOO=bar", k)
			}
			d.env = append(d.env, fmt.Sprintf("%s=%s", envName, v))
		default:
			return nil, errors.Errorf("invalid driver option %s for docker-container driver", k)
		}
	}

	return d, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}
