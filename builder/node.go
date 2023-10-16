package builder

import (
	"context"
	"sort"

	"github.com/docker/buildx/driver"
	ctxkube "github.com/docker/buildx/driver/kubernetes/context"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/platformutil"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/grpcerrors"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
)

type Node struct {
	store.Node
	Builder     string
	Driver      *driver.DriverHandle
	DriverInfo  *driver.Info
	ImageOpt    imagetools.Opt
	ProxyConfig map[string]string
	Version     string
	Err         error

	// worker settings
	IDs       []string
	Platforms []ocispecs.Platform
	GCPolicy  []client.PruneInfo
	Labels    map[string]string
}

// Nodes returns nodes for this builder.
func (b *Builder) Nodes() []Node {
	return b.nodes
}

type LoadNodesOption func(*loadNodesOptions)

type loadNodesOptions struct {
	data     bool
	dialMeta map[string][]string
}

func WithData() LoadNodesOption {
	return func(o *loadNodesOptions) {
		o.data = true
	}
}

func WithDialMeta(dialMeta map[string][]string) LoadNodesOption {
	return func(o *loadNodesOptions) {
		o.dialMeta = dialMeta
	}
}

// LoadNodes loads and returns nodes for this builder.
// TODO: this should be a method on a Node object and lazy load data for each driver.
func (b *Builder) LoadNodes(ctx context.Context, opts ...LoadNodesOption) (_ []Node, err error) {
	lno := loadNodesOptions{
		data: false,
	}
	for _, opt := range opts {
		opt(&lno)
	}

	eg, _ := errgroup.WithContext(ctx)
	b.nodes = make([]Node, len(b.NodeGroup.Nodes))

	defer func() {
		if b.err == nil && err != nil {
			b.err = err
		}
	}()

	factory, err := b.Factory(ctx, lno.dialMeta)
	if err != nil {
		return nil, err
	}

	imageopt, err := b.ImageOpt()
	if err != nil {
		return nil, err
	}

	for i, n := range b.NodeGroup.Nodes {
		func(i int, n store.Node) {
			eg.Go(func() error {
				node := Node{
					Node:        n,
					ProxyConfig: storeutil.GetProxyConfig(b.opts.dockerCli),
					Platforms:   n.Platforms,
					Builder:     b.Name,
				}
				defer func() {
					b.nodes[i] = node
				}()

				dockerapi, err := dockerutil.NewClientAPI(b.opts.dockerCli, n.Endpoint)
				if err != nil {
					node.Err = err
					return nil
				}

				contextStore := b.opts.dockerCli.ContextStore()

				var kcc driver.KubeClientConfig
				kcc, err = ctxkube.ConfigFromEndpoint(n.Endpoint, contextStore)
				if err != nil {
					// err is returned if n.Endpoint is non-context name like "unix:///var/run/docker.sock".
					// try again with name="default".
					// FIXME(@AkihiroSuda): n should retain real context name.
					kcc, err = ctxkube.ConfigFromEndpoint("default", contextStore)
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

				d, err := driver.GetDriver(ctx, "buildx_buildkit_"+n.Name, factory, n.Endpoint, dockerapi, imageopt.Auth, kcc, n.Flags, n.Files, n.DriverOpts, n.Platforms, b.opts.contextPathHash, lno.dialMeta)
				if err != nil {
					node.Err = err
					return nil
				}
				node.Driver = d
				node.ImageOpt = imageopt

				if lno.data {
					if err := node.loadData(ctx); err != nil {
						node.Err = err
					}
				}
				return nil
			})
		}(i, n)
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// TODO: This should be done in the routine loading driver data
	if lno.data {
		kubernetesDriverCount := 0
		for _, d := range b.nodes {
			if d.DriverInfo != nil && len(d.DriverInfo.DynamicNodes) > 0 {
				kubernetesDriverCount++
			}
		}

		isAllKubernetesDrivers := len(b.nodes) == kubernetesDriverCount
		if isAllKubernetesDrivers {
			var nodes []Node
			var dynamicNodes []store.Node
			for _, di := range b.nodes {
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
			b.nodes = nodes
			b.NodeGroup.Dynamic = true
		}
	}

	return b.nodes, nil
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
		for idx, w := range workers {
			n.IDs = append(n.IDs, w.ID)
			n.Platforms = append(n.Platforms, w.Platforms...)
			if idx == 0 {
				n.GCPolicy = w.GCPolicy
				n.Labels = w.Labels
			}
		}
		sort.Strings(n.IDs)
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
