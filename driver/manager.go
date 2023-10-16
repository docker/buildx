package driver

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"

	dockerclient "github.com/docker/docker/client"
	"github.com/moby/buildkit/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"k8s.io/client-go/rest"
)

type Factory interface {
	Name() string
	Usage() string
	Priority(ctx context.Context, endpoint string, api dockerclient.APIClient, dialMeta map[string][]string) int
	New(ctx context.Context, cfg InitConfig) (Driver, error)
	AllowsInstances() bool
}

type BuildkitConfig struct {
	// Entitlements []string
	// Rootless bool
}

type KubeClientConfig interface {
	ClientConfig() (*rest.Config, error)
	Namespace() (string, bool, error)
}

type KubeClientConfigInCluster struct{}

func (k KubeClientConfigInCluster) ClientConfig() (*rest.Config, error) {
	return rest.InClusterConfig()
}

func (k KubeClientConfigInCluster) Namespace() (string, bool, error) {
	namespace, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(namespace)), true, nil
}

type InitConfig struct {
	// This object needs updates to be generic for different drivers
	Name             string
	EndpointAddr     string
	DockerAPI        dockerclient.APIClient
	KubeClientConfig KubeClientConfig
	BuildkitFlags    []string
	Files            map[string][]byte
	DriverOpts       map[string]string
	Auth             Auth
	Platforms        []specs.Platform
	ContextPathHash  string // can be used for determining pods in the driver instance
	DialMeta         map[string][]string
}

var drivers map[string]Factory

func Register(f Factory) {
	if drivers == nil {
		drivers = map[string]Factory{}
	}
	drivers[f.Name()] = f
}

func GetDefaultFactory(ctx context.Context, ep string, c dockerclient.APIClient, instanceRequired bool, dialMeta map[string][]string) (Factory, error) {
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
		dd = append(dd, p{f: f, priority: f.Priority(ctx, ep, c, dialMeta)})
	}
	sort.Slice(dd, func(i, j int) bool {
		return dd[i].priority < dd[j].priority
	})
	return dd[0].f, nil
}

func GetFactory(name string, instanceRequired bool) (Factory, error) {
	for _, f := range drivers {
		if f.Name() == name {
			if instanceRequired && !f.AllowsInstances() {
				return nil, errors.Errorf("additional instances of driver %q cannot be created", name)
			}
			return f, nil
		}
	}
	return nil, errors.Errorf("failed to find driver %q", name)
}

func GetDriver(ctx context.Context, name string, f Factory, endpointAddr string, api dockerclient.APIClient, auth Auth, kcc KubeClientConfig, flags []string, files map[string][]byte, do map[string]string, platforms []specs.Platform, contextPathHash string, dialMeta map[string][]string) (*DriverHandle, error) {
	ic := InitConfig{
		EndpointAddr:     endpointAddr,
		DockerAPI:        api,
		KubeClientConfig: kcc,
		Name:             name,
		BuildkitFlags:    flags,
		DriverOpts:       do,
		Auth:             auth,
		Platforms:        platforms,
		ContextPathHash:  contextPathHash,
		DialMeta:         dialMeta,
		Files:            files,
	}
	if f == nil {
		var err error
		f, err = GetDefaultFactory(ctx, endpointAddr, api, false, dialMeta)
		if err != nil {
			return nil, err
		}
	}
	d, err := f.New(ctx, ic)
	if err != nil {
		return nil, err
	}
	return &DriverHandle{Driver: d}, nil
}

func GetFactories(instanceRequired bool) []Factory {
	ds := make([]Factory, 0, len(drivers))
	for _, d := range drivers {
		if instanceRequired && !d.AllowsInstances() {
			continue
		}
		ds = append(ds, d)
	}
	sort.Slice(ds, func(i, j int) bool {
		return ds[i].Name() < ds[j].Name()
	})
	return ds
}

type DriverHandle struct {
	Driver
	client                  *client.Client
	err                     error
	once                    sync.Once
	historyAPISupportedOnce sync.Once
	historyAPISupported     bool
}

func (d *DriverHandle) Client(ctx context.Context) (*client.Client, error) {
	d.once.Do(func() {
		d.client, d.err = d.Driver.Client(ctx)
	})
	return d.client, d.err
}

func (d *DriverHandle) HistoryAPISupported(ctx context.Context) bool {
	d.historyAPISupportedOnce.Do(func() {
		if c, err := d.Client(ctx); err == nil {
			d.historyAPISupported = historyAPISupported(ctx, c)
		}
	})
	return d.historyAPISupported
}
