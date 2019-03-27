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
	Priority() int // take initConfig?
	New(ctx context.Context, cfg InitConfig) (Driver, error)
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

func GetDefaultFactory() (Factory, error) {
	if len(drivers) == 0 {
		return nil, errors.Errorf("no drivers available")
	}
	type p struct {
		f        Factory
		priority int
	}
	dd := make([]p, 0, len(drivers))
	for _, f := range drivers {
		dd = append(dd, p{f: f, priority: f.Priority()})
	}
	sort.Slice(dd, func(i, j int) bool {
		return dd[i].priority < dd[j].priority
	})
	return dd[0].f, nil
}

func GetDriver(ctx context.Context, name string, f Factory, api dockerclient.APIClient) (Driver, error) {
	if f == nil {
		var err error
		f, err = GetDefaultFactory()
		if err != nil {
			return nil, err
		}
	}
	return f.New(ctx, InitConfig{
		Name:      name,
		DockerAPI: api,
	})
}
