package builder

import (
	"context"
	"sync"

	"github.com/docker/buildx/driver"
	ctxkube "github.com/docker/buildx/driver/kubernetes/context"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/platformutil"
	"github.com/moby/buildkit/util/grpcerrors"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
)

type Node struct {
	store.Node
	Driver      driver.Driver
	DriverInfo  *driver.Info
	Platforms   []ocispecs.Platform
	ImageOpt    imagetools.Opt
	ProxyConfig map[string]string
	Version     string
	Err         error
}

type nodesFactory struct {
	nodes []Node
	once  sync.Once
}

// Nodes loads and returns nodes for this builder.
func (b *Builder) Nodes(ctx context.Context) (_ []Node, err error) {
	b.nodesFactory.once.Do(func() {
		err = b.loadNodes(ctx)
	})
	return b.nodesFactory.nodes, err
}

// NodesWithData loads nodes with data for this builder.
func (b *Builder) NodesWithData(ctx context.Context) (_ []Node, err error) {
	err = b.loadNodesWithData(ctx)
	return b.nodesFactory.nodes, err
}

func (b *Builder) loadNodes(ctx context.Context) (err error) {
	b.nodesFactory.nodes = make([]Node, len(b.NodeGroup.Nodes))

	defer func() {
		if b.err == nil && err != nil {
			b.err = err
		}
	}()

	factory, err := b.Factory(ctx)
	if err != nil {
		return err
	}

	imageopt, err := b.ImageOpt()
	if err != nil {
		return err
	}

	eg, _ := errgroup.WithContext(ctx)
	for i, n := range b.NodeGroup.Nodes {
		func(i int, n store.Node) {
			eg.Go(func() error {
				node := Node{
					Node:        n,
					ProxyConfig: storeutil.GetProxyConfig(b.opts.dockerCli),
				}
				defer func() {
					b.nodesFactory.nodes[i] = node
				}()

				dockerapi, err := dockerutil.NewClientAPI(b.opts.dockerCli, n.Endpoint)
				if err != nil {
					node.Err = err
					return nil
				}

				contextStore := b.opts.dockerCli.ContextStore()

				var kcc driver.KubeClientConfig
				kcc, err = ctxkube.ConfigFromContext(n.Endpoint, contextStore)
				if err != nil {
					// err is returned if n.Endpoint is non-context name like "unix:///var/run/docker.sock".
					// try again with name="default".
					// FIXME(@AkihiroSuda): n should retain real context name.
					kcc, err = ctxkube.ConfigFromContext("default", contextStore)
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

				d, err := driver.GetDriver(ctx, "buildx_buildkit_"+n.Name, factory, n.Endpoint, dockerapi, imageopt.Auth, kcc, n.Flags, n.Files, n.DriverOpts, n.Platforms, b.opts.contextPathHash)
				if err != nil {
					node.Err = err
					return nil
				}

				node.Driver = d
				node.ImageOpt = imageopt
				return nil
			})
		}(i, n)
	}

	return eg.Wait()
}

func (b *Builder) loadNodesWithData(ctx context.Context) (err error) {
	// ensure nodes are loaded
	if _, err = b.Nodes(ctx); err != nil {
		return err
	}

	eg, _ := errgroup.WithContext(ctx)
	for idx := range b.nodesFactory.nodes {
		func(idx int) {
			eg.Go(func() error {
				if err = b.nodesFactory.nodes[idx].loadData(ctx); err != nil {
					b.nodesFactory.nodes[idx].Err = err
				}
				return nil
			})
		}(idx)
	}
	if err = eg.Wait(); err != nil {
		return err
	}

	kubernetesDriverCount := 0
	for _, d := range b.nodesFactory.nodes {
		if d.DriverInfo != nil && len(d.DriverInfo.DynamicNodes) > 0 {
			kubernetesDriverCount++
		}
	}

	isAllKubernetesDrivers := len(b.nodesFactory.nodes) == kubernetesDriverCount
	if isAllKubernetesDrivers {
		var nodes []Node
		var dynamicNodes []store.Node
		for _, di := range b.nodesFactory.nodes {
			// dynamic nodes are used in Kubernetes driver.
			// Kubernetes' pods are dynamically mapped to BuildKit Nodes.
			if di.DriverInfo != nil && len(di.DriverInfo.DynamicNodes) > 0 {
				for i := 0; i < len(di.DriverInfo.DynamicNodes); i++ {
					diClone := di
					if pl := di.DriverInfo.DynamicNodes[i].Platforms; len(pl) > 0 {
						diClone.Platforms = pl
					}
					nodes = append(nodes, di)
				}
				dynamicNodes = append(dynamicNodes, di.DriverInfo.DynamicNodes...)
			}
		}

		// not append (remove the static nodes in the store)
		b.NodeGroup.Nodes = dynamicNodes
		b.nodesFactory.nodes = nodes
		b.NodeGroup.Dynamic = true
	}

	return nil
}

func (n *Node) loadData(ctx context.Context) error {
	if n.Driver == nil {
		return nil
	}
	info, err := n.Driver.Info(ctx)
	if err != nil {
		return err
	}
	n.DriverInfo = info
	if n.DriverInfo.Status == driver.Running {
		driverClient, err := n.Driver.Client(ctx)
		if err != nil {
			return err
		}
		workers, err := driverClient.ListWorkers(ctx)
		if err != nil {
			return errors.Wrap(err, "listing workers")
		}
		for _, w := range workers {
			n.Platforms = append(n.Platforms, w.Platforms...)
		}
		n.Platforms = platformutil.Dedupe(n.Platforms)
		inf, err := driverClient.Info(ctx)
		if err != nil {
			if st, ok := grpcerrors.AsGRPCStatus(err); ok && st.Code() == codes.Unimplemented {
				n.Version, err = n.Driver.Version(ctx)
				if err != nil {
					return errors.Wrap(err, "getting version")
				}
			}
		} else {
			n.Version = inf.BuildkitVersion.Version
		}
	}
	return nil
}
