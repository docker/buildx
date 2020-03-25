package commands

import (
	"context"
	"os"
	"path/filepath"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/docker"
	"github.com/docker/cli/cli/context/kubernetes"
	dopts "github.com/docker/cli/opts"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// getStore returns current builder instance store
func getStore(dockerCli command.Cli) (*store.Txn, func(), error) {
	dir := filepath.Dir(dockerCli.ConfigFile().Filename)
	s, err := store.New(dir)
	if err != nil {
		return nil, nil, err
	}
	return s.Txn()
}

// getCurrentEndpoint returns the current default endpoint value
func getCurrentEndpoint(dockerCli command.Cli) (string, error) {
	name := dockerCli.CurrentContext()
	if name != "default" {
		return name, nil
	}
	de, err := getDockerEndpoint(dockerCli, name)
	if err != nil {
		return "", errors.Errorf("docker endpoint for %q not found", name)
	}
	return de, nil
}

// getDockerEndpoint returns docker endpoint string for given context
func getDockerEndpoint(dockerCli command.Cli, name string) (string, error) {
	list, err := dockerCli.ContextStore().List()
	if err != nil {
		return "", err
	}
	for _, l := range list {
		if l.Name == name {
			ep, ok := l.Endpoints["docker"]
			if !ok {
				return "", errors.Errorf("context %q does not have a Docker endpoint", name)
			}
			typed, ok := ep.(docker.EndpointMeta)
			if !ok {
				return "", errors.Errorf("endpoint %q is not of type EndpointMeta, %T", ep, ep)
			}
			return typed.Host, nil
		}
	}
	return "", nil
}

// validateEndpoint validates that endpoint is either a context or a docker host
func validateEndpoint(dockerCli command.Cli, ep string) (string, error) {
	de, err := getDockerEndpoint(dockerCli, ep)
	if err == nil && de != "" {
		if ep == "default" {
			return de, nil
		}
		return ep, nil
	}
	h, err := dopts.ParseHost(true, ep)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse endpoint %s", ep)
	}
	return h, nil
}

// getCurrentInstance finds the current builder instance
func getCurrentInstance(txn *store.Txn, dockerCli command.Cli) (*store.NodeGroup, error) {
	ep, err := getCurrentEndpoint(dockerCli)
	if err != nil {
		return nil, err
	}
	ng, err := txn.Current(ep)
	if err != nil {
		return nil, err
	}
	if ng == nil {
		ng, _ = getNodeGroup(txn, dockerCli, dockerCli.CurrentContext())
	}

	return ng, nil
}

// getNodeGroup returns nodegroup based on the name
func getNodeGroup(txn *store.Txn, dockerCli command.Cli, name string) (*store.NodeGroup, error) {
	ng, err := txn.NodeGroupByName(name)
	if err != nil {
		if !os.IsNotExist(errors.Cause(err)) {
			return nil, err
		}
	}
	if ng != nil {
		return ng, nil
	}

	if name == "default" {
		name = dockerCli.CurrentContext()
	}

	list, err := dockerCli.ContextStore().List()
	if err != nil {
		return nil, err
	}
	for _, l := range list {
		if l.Name == name {
			return &store.NodeGroup{
				Name: "default",
				Nodes: []store.Node{
					{
						Name:     "default",
						Endpoint: name,
					},
				},
			}, nil
		}
	}

	return nil, errors.Errorf("no builder %q found", name)
}

// driversForNodeGroup returns drivers for a nodegroup instance
func driversForNodeGroup(ctx context.Context, dockerCli command.Cli, ng *store.NodeGroup, contextPathHash string) ([]build.DriverInfo, error) {
	eg, _ := errgroup.WithContext(ctx)

	dis := make([]build.DriverInfo, len(ng.Nodes))

	var f driver.Factory
	if ng.Driver != "" {
		f = driver.GetFactory(ng.Driver, true)
		if f == nil {
			return nil, errors.Errorf("failed to find driver %q", f)
		}
	} else {
		dockerapi, err := clientForEndpoint(dockerCli, ng.Nodes[0].Endpoint)
		if err != nil {
			return nil, err
		}
		f, err = driver.GetDefaultFactory(ctx, dockerapi, false)
		if err != nil {
			return nil, err
		}
		ng.Driver = f.Name()
	}

	for i, n := range ng.Nodes {
		func(i int, n store.Node) {
			eg.Go(func() error {
				di := build.DriverInfo{
					Name:     n.Name,
					Platform: n.Platforms,
				}
				defer func() {
					dis[i] = di
				}()
				dockerapi, err := clientForEndpoint(dockerCli, n.Endpoint)
				if err != nil {
					di.Err = err
					return nil
				}
				// TODO: replace the following line with dockerclient.WithAPIVersionNegotiation option in clientForEndpoint
				dockerapi.NegotiateAPIVersion(ctx)

				contextStore := dockerCli.ContextStore()
				kcc, err := kubernetes.ConfigFromContext(n.Endpoint, contextStore)
				if err != nil {
					// err is returned if n.Endpoint is non-context name like "unix:///var/run/docker.sock".
					// try again with name="default".
					// FIXME: n should retain real context name.
					kcc, err = kubernetes.ConfigFromContext("default", contextStore)
					if err != nil {
						logrus.Error(err)
					}
				}
				d, err := driver.GetDriver(ctx, "buildx_buildkit_"+n.Name, f, dockerapi, kcc, n.Flags, n.ConfigFile, n.DriverOpts, contextPathHash)
				if err != nil {
					di.Err = err
					return nil
				}
				di.Driver = d
				return nil
			})
		}(i, n)
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return dis, nil
}

// clientForEndpoint returns a docker client for an endpoint
func clientForEndpoint(dockerCli command.Cli, name string) (dockerclient.APIClient, error) {
	list, err := dockerCli.ContextStore().List()
	if err != nil {
		return nil, err
	}
	for _, l := range list {
		if l.Name == name {
			dep, ok := l.Endpoints["docker"]
			if !ok {
				return nil, errors.Errorf("context %q does not have a Docker endpoint", name)
			}
			epm, ok := dep.(docker.EndpointMeta)
			if !ok {
				return nil, errors.Errorf("endpoint %q is not of type EndpointMeta, %T", dep, dep)
			}
			ep, err := docker.WithTLSData(dockerCli.ContextStore(), name, epm)
			if err != nil {
				return nil, err
			}
			clientOpts, err := ep.ClientOpts()
			if err != nil {
				return nil, err
			}
			return dockerclient.NewClientWithOpts(clientOpts...)
		}
	}

	ep := docker.Endpoint{
		EndpointMeta: docker.EndpointMeta{
			Host: name,
		},
	}

	clientOpts, err := ep.ClientOpts()
	if err != nil {
		return nil, err
	}

	return dockerclient.NewClientWithOpts(clientOpts...)
}

func getInstanceOrDefault(ctx context.Context, dockerCli command.Cli, instance, contextPathHash string) ([]build.DriverInfo, error) {
	if instance != "" {
		return getInstanceByName(ctx, dockerCli, instance, contextPathHash)
	}
	return getDefaultDrivers(ctx, dockerCli, contextPathHash)
}

func getInstanceByName(ctx context.Context, dockerCli command.Cli, instance, contextPathHash string) ([]build.DriverInfo, error) {
	txn, release, err := getStore(dockerCli)
	if err != nil {
		return nil, err
	}
	defer release()

	ng, err := txn.NodeGroupByName(instance)
	if err != nil {
		return nil, err
	}
	return driversForNodeGroup(ctx, dockerCli, ng, contextPathHash)
}

// getDefaultDrivers returns drivers based on current cli config
func getDefaultDrivers(ctx context.Context, dockerCli command.Cli, contextPathHash string) ([]build.DriverInfo, error) {
	txn, release, err := getStore(dockerCli)
	if err != nil {
		return nil, err
	}
	defer release()

	ng, err := getCurrentInstance(txn, dockerCli)
	if err != nil {
		return nil, err
	}

	if ng != nil {
		return driversForNodeGroup(ctx, dockerCli, ng, contextPathHash)
	}

	d, err := driver.GetDriver(ctx, "buildx_buildkit_default", nil, dockerCli.Client(), nil, nil, "", nil, contextPathHash)
	if err != nil {
		return nil, err
	}
	return []build.DriverInfo{
		{
			Name:   "default",
			Driver: d,
		},
	}, nil
}

func loadInfoData(ctx context.Context, d *dinfo) error {
	if d.di.Driver == nil {
		return nil
	}
	info, err := d.di.Driver.Info(ctx)
	if err != nil {
		return err
	}
	d.info = info
	if info.Status == driver.Running {
		c, err := d.di.Driver.Client(ctx)
		if err != nil {
			return err
		}
		workers, err := c.ListWorkers(ctx)
		if err != nil {
			return errors.Wrap(err, "listing workers")
		}
		for _, w := range workers {
			for _, p := range w.Platforms {
				d.platforms = append(d.platforms, p)
			}
		}
		d.platforms = platformutil.Dedupe(d.platforms)
	}
	return nil
}

func loadNodeGroupData(ctx context.Context, dockerCli command.Cli, ngi *nginfo) error {
	eg, _ := errgroup.WithContext(ctx)

	dis, err := driversForNodeGroup(ctx, dockerCli, ngi.ng, "")
	if err != nil {
		return err
	}
	ngi.drivers = make([]dinfo, len(dis))
	for i, di := range dis {
		d := di
		ngi.drivers[i].di = &d
		func(d *dinfo) {
			eg.Go(func() error {
				if err := loadInfoData(ctx, d); err != nil {
					d.err = err
				}
				return nil
			})
		}(&ngi.drivers[i])
	}

	if eg.Wait(); err != nil {
		return err
	}
	for _, di := range ngi.drivers {
		// dynamic nodes are used in Kubernetes driver.
		// Kubernetes pods are dynamically mapped to BuildKit Nodes.
		if di.info != nil && len(di.info.DynamicNodes) > 0 {
			var drivers []dinfo
			for i := 0; i < len(di.info.DynamicNodes); i++ {
				// all []dinfo share *build.DriverInfo and *driver.Info
				diClone := di
				if pl := di.info.DynamicNodes[i].Platforms; len(pl) > 0 {
					diClone.platforms = pl
				}
				drivers = append(drivers, di)
			}
			// not append (remove the static nodes in the store)
			ngi.ng.Nodes = di.info.DynamicNodes
			ngi.ng.Dynamic = true
			ngi.drivers = drivers
			return nil
		}
	}
	return nil
}

func dockerAPI(dockerCli command.Cli) *api {
	return &api{dockerCli: dockerCli}
}

type api struct {
	dockerCli command.Cli
}

func (a *api) DockerAPI(name string) (dockerclient.APIClient, error) {
	if name == "" {
		name = a.dockerCli.CurrentContext()
	}
	return clientForEndpoint(a.dockerCli, name)
}
