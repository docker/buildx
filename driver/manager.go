package driver

import (
	"context"
	"sort"

	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
)

type Factory interface {
	Name() string
	Usage() string
	Priority(context.Context, dockerclient.APIClient) int
	New(ctx context.Context, cfg InitConfig) (Driver, error)
	AllowsInstances() bool
}

type BuildkitConfig struct {
	// Entitlements []string
	// Rootless bool
}

type InitConfig struct {
	// This object needs updates to be generic for different drivers
	Name           string
	DockerAPI      dockerclient.APIClient
	BuildkitConfig BuildkitConfig
	Meta           map[string]interface{}
}

var drivers map[string]Factory

func Register(f Factory) {
	if drivers == nil {
		drivers = map[string]Factory{}
	}
	drivers[f.Name()] = f
}

func GetDefaultFactory(ctx context.Context, c dockerclient.APIClient, instanceRequired bool) (Factory, error) {
	if len(drivers) == 0 {
		return nil, errors.Errorf("no drivers available")
	}
	type p struct {
		f        Factory
		priority int
	}
	dd := make([]p, 0, len(drivers))
	for _, f := range drivers {
		if instanceRequired && !f.AllowsInstances() {
			continue
		}
		dd = append(dd, p{f: f, priority: f.Priority(ctx, c)})
	}
	sort.Slice(dd, func(i, j int) bool {
		return dd[i].priority < dd[j].priority
	})
	return dd[0].f, nil
}

func GetFactory(name string, instanceRequired bool) Factory {
	for _, f := range drivers {
		if instanceRequired && !f.AllowsInstances() {
			continue
		}
		if f.Name() == name {
			return f
		}
	}
	return nil
}

func GetDriver(ctx context.Context, name string, f Factory, api dockerclient.APIClient) (Driver, error) {
	ic := InitConfig{
		DockerAPI: api,
		Name:      name,
	}
	if f == nil {
		var err error
		f, err = GetDefaultFactory(ctx, api, false)
		if err != nil {
			return nil, err
		}
	}
	return f.New(ctx, ic)
}
