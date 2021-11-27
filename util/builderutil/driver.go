package builderutil

import (
	"context"

	"github.com/docker/buildx/driver"
	k8sconfig "github.com/docker/buildx/driver/kubernetes/config"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/platformutil"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type Driver struct {
	Name      string
	Driver    driver.Driver
	Info      *driver.Info
	Platforms []ocispecs.Platform
	ImageOpt  imagetools.Opt
	Err       error
}

// LoadDrivers loads drivers.
func (b *Builder) LoadDrivers(ctx context.Context, withData bool, contextPathHash string) error {
	eg, _ := errgroup.WithContext(ctx)
	b.Drivers = make([]Driver, len(b.NodeGroup.Nodes))

	var f driver.Factory
	if b.NodeGroup.Driver != "" {
		f = driver.GetFactory(b.NodeGroup.Driver, true)
		if f == nil {
			return errors.Errorf("failed to find driver %q", f)
		}
	} else {
		dockerapi, err := storeutil.ClientForEndpoint(b.dockerCli, b.NodeGroup.Nodes[0].Endpoint)
		if err != nil {
			return err
		}
		f, err = driver.GetDefaultFactory(ctx, dockerapi, false)
		if err != nil {
			return err
		}
		b.NodeGroup.Driver = f.Name()
	}

	imageopt, err := b.GetImageOpt()
	if err != nil {
		return err
	}

	for i, n := range b.NodeGroup.Nodes {
		func(i int, n store.Node) {
			eg.Go(func() error {
				di := Driver{
					Name:      n.Name,
					Platforms: n.Platforms,
				}
				defer func() {
					b.Drivers[i] = di
				}()

				dockerapi, err := storeutil.ClientForEndpoint(b.dockerCli, n.Endpoint)
				if err != nil {
					di.Err = err
					return nil
				}

				contextStore := b.dockerCli.ContextStore()

				var kcc driver.KubeClientConfig
				kcc, err = k8sconfig.FromContext(n.Endpoint, contextStore)
				if err != nil {
					// err is returned if n.Endpoint is non-context name like "unix:///var/run/docker.sock".
					// try again with name="default".
					// FIXME(@AkihiroSuda): n should retain real context name.
					kcc, err = k8sconfig.FromContext("default", contextStore)
					if err != nil {
						logrus.Error(err)
					}
				}

				tryToUseKubeConfigInCluster := false
				if kcc == nil {
					tryToUseKubeConfigInCluster = true
				} else {
					if _, err := kcc.ClientConfig(); err != nil {
						tryToUseKubeConfigInCluster = true
					}
				}
				if tryToUseKubeConfigInCluster {
					kccInCluster := driver.KubeClientConfigInCluster{}
					if _, err := kccInCluster.ClientConfig(); err == nil {
						logrus.Debug("using kube config in cluster")
						kcc = kccInCluster
					}
				}

				d, err := driver.GetDriver(ctx, "buildx_buildkit_"+n.Name, f, dockerapi, imageopt.Auth, kcc, n.Flags, n.Files, n.DriverOpts, n.Platforms, contextPathHash)
				if err != nil {
					di.Err = err
					return nil
				}
				di.Driver = d
				di.ImageOpt = imageopt

				if withData {
					if err := di.loadData(ctx); err != nil {
						di.Err = err
					}
				}
				return nil
			})
		}(i, n)
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	if withData {
		kubernetesDriverCount := 0
		for _, d := range b.Drivers {
			if d.Info != nil && len(d.Info.DynamicNodes) > 0 {
				kubernetesDriverCount++
			}
		}

		isAllKubernetesDrivers := len(b.Drivers) == kubernetesDriverCount
		if isAllKubernetesDrivers {
			var drivers []Driver
			var dynamicNodes []store.Node
			for _, di := range b.Drivers {
				// dynamic nodes are used in Kubernetes driver.
				// Kubernetes pods are dynamically mapped to BuildKit Nodes.
				if di.Info != nil && len(di.Info.DynamicNodes) > 0 {
					for i := 0; i < len(di.Info.DynamicNodes); i++ {
						// all []dinfo share *build.DriverInfo and *driver.Info
						diClone := di
						if pl := di.Info.DynamicNodes[i].Platforms; len(pl) > 0 {
							diClone.Platforms = pl
						}
						drivers = append(drivers, di)
					}
					dynamicNodes = append(dynamicNodes, di.Info.DynamicNodes...)
				}
			}

			// not append (remove the static nodes in the store)
			b.NodeGroup.Nodes = dynamicNodes
			b.Drivers = drivers
			b.NodeGroup.Dynamic = true
		}
	}

	return nil
}

func (d *Driver) loadData(ctx context.Context) error {
	if d.Driver == nil {
		return nil
	}
	info, err := d.Driver.Info(ctx)
	if err != nil {
		return err
	}
	d.Info = info
	if d.Info.Status == driver.Running {
		cdriver, err := d.Driver.Client(ctx)
		if err != nil {
			return err
		}
		workers, err := cdriver.ListWorkers(ctx)
		if err != nil {
			return errors.Wrap(err, "listing workers")
		}
		for _, w := range workers {
			d.Platforms = append(d.Platforms, w.Platforms...)
		}
		d.Platforms = platformutil.Dedupe(d.Platforms)
	}
	return nil
}
