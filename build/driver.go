package build

import (
	"context"
	"fmt"
	"sync"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/util/flightcontrol"
	"github.com/moby/buildkit/util/tracing"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type resolvedNode struct {
	resolver    *nodeResolver
	driverIndex int
	platforms   []specs.Platform
}

func (dp resolvedNode) Node() builder.Node {
	return dp.resolver.nodes[dp.driverIndex]
}

func (dp resolvedNode) Client(ctx context.Context) (*client.Client, error) {
	clients, err := dp.resolver.boot(ctx, []int{dp.driverIndex}, nil)
	if err != nil {
		return nil, err
	}
	return clients[0], nil
}

func (dp resolvedNode) BuildOpts(ctx context.Context) (gateway.BuildOpts, error) {
	opts, err := dp.resolver.opts(ctx, []int{dp.driverIndex}, nil)
	if err != nil {
		return gateway.BuildOpts{}, err
	}
	return opts[0], nil
}

type matchMaker func(specs.Platform) platforms.MatchComparer

type cachedGroup[T any] struct {
	g       flightcontrol.Group[T]
	cache   map[int]T
	cacheMu sync.Mutex
}

func newCachedGroup[T any]() cachedGroup[T] {
	return cachedGroup[T]{
		cache: map[int]T{},
	}
}

type nodeResolver struct {
	nodes     []builder.Node
	clients   cachedGroup[*client.Client]
	buildOpts cachedGroup[gateway.BuildOpts]
}

func resolveDrivers(ctx context.Context, nodes []builder.Node, opt map[string]Options, pw progress.Writer) (map[string][]*resolvedNode, error) {
	driverRes := newDriverResolver(nodes)
	drivers, err := driverRes.Resolve(ctx, opt, pw)
	if err != nil {
		return nil, err
	}
	return drivers, err
}

func newDriverResolver(nodes []builder.Node) *nodeResolver {
	r := &nodeResolver{
		nodes:     nodes,
		clients:   newCachedGroup[*client.Client](),
		buildOpts: newCachedGroup[gateway.BuildOpts](),
	}
	return r
}

func (r *nodeResolver) Resolve(ctx context.Context, opt map[string]Options, pw progress.Writer) (map[string][]*resolvedNode, error) {
	if len(r.nodes) == 0 {
		return nil, nil
	}

	nodes := map[string][]*resolvedNode{}
	for k, opt := range opt {
		node, perfect, err := r.resolve(ctx, opt.Platforms, pw, platforms.OnlyStrict, nil)
		if err != nil {
			return nil, err
		}
		if !perfect {
			break
		}
		nodes[k] = node
	}
	if len(nodes) != len(opt) {
		// if we didn't get a perfect match, we need to boot all drivers
		allIndexes := make([]int, len(r.nodes))
		for i := range allIndexes {
			allIndexes[i] = i
		}

		clients, err := r.boot(ctx, allIndexes, pw)
		if err != nil {
			return nil, err
		}
		eg, egCtx := errgroup.WithContext(ctx)
		workers := make([][]specs.Platform, len(clients))
		for i, c := range clients {
			i, c := i, c
			if c == nil {
				continue
			}
			eg.Go(func() error {
				ww, err := c.ListWorkers(egCtx)
				if err != nil {
					return errors.Wrap(err, "listing workers")
				}

				ps := make(map[string]specs.Platform, len(ww))
				for _, w := range ww {
					for _, p := range w.Platforms {
						pk := platforms.Format(platforms.Normalize(p))
						ps[pk] = p
					}
				}
				for _, p := range ps {
					workers[i] = append(workers[i], p)
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return nil, err
		}

		// then we can attempt to match against all the available platforms
		// (this time we don't care about imperfect matches)
		nodes = map[string][]*resolvedNode{}
		for k, opt := range opt {
			node, _, err := r.resolve(ctx, opt.Platforms, pw, platforms.Only, func(idx int, n builder.Node) []specs.Platform {
				return workers[idx]
			})
			if err != nil {
				return nil, err
			}
			nodes[k] = node
		}
	}

	idxs := make([]int, 0, len(r.nodes))
	for _, nodes := range nodes {
		for _, node := range nodes {
			idxs = append(idxs, node.driverIndex)
		}
	}

	// preload capabilities
	span, ctx := tracing.StartSpan(ctx, "load buildkit capabilities", trace.WithSpanKind(trace.SpanKindInternal))
	_, err := r.opts(ctx, idxs, pw)
	tracing.FinishWithError(span, err)
	if err != nil {
		return nil, err
	}

	return nodes, nil
}

func (r *nodeResolver) resolve(ctx context.Context, ps []specs.Platform, pw progress.Writer, matcher matchMaker, additional func(idx int, n builder.Node) []specs.Platform) ([]*resolvedNode, bool, error) {
	if len(r.nodes) == 0 {
		return nil, true, nil
	}

	perfect := true
	nodeIdxs := make([]int, 0)
	for _, p := range ps {
		idx := r.get(p, matcher, additional)
		if idx == -1 {
			idx = 0
			perfect = false
		}
		nodeIdxs = append(nodeIdxs, idx)
	}

	var nodes []*resolvedNode
	if len(nodeIdxs) == 0 {
		nodes = append(nodes, &resolvedNode{
			resolver:    r,
			driverIndex: 0,
		})
		nodeIdxs = append(nodeIdxs, 0)
	} else {
		for i, idx := range nodeIdxs {
			node := &resolvedNode{
				resolver:    r,
				driverIndex: idx,
			}
			if len(ps) > 0 {
				node.platforms = []specs.Platform{ps[i]}
			}
			nodes = append(nodes, node)
		}
	}

	nodes = recombineNodes(nodes)
	if _, err := r.boot(ctx, nodeIdxs, pw); err != nil {
		return nil, false, err
	}
	return nodes, perfect, nil
}

func (r *nodeResolver) get(p specs.Platform, matcher matchMaker, additionalPlatforms func(int, builder.Node) []specs.Platform) int {
	best := -1
	bestPlatform := specs.Platform{}
	for i, node := range r.nodes {
		platforms := node.Platforms
		if additionalPlatforms != nil {
			platforms = append([]specs.Platform{}, platforms...)
			platforms = append(platforms, additionalPlatforms(i, node)...)
		}
		for _, p2 := range platforms {
			m := matcher(p2)
			if !m.Match(p) {
				continue
			}

			if best == -1 {
				best = i
				bestPlatform = p2
				continue
			}
			if matcher(p2).Less(p, bestPlatform) {
				best = i
				bestPlatform = p2
			}
		}
	}
	return best
}

func (r *nodeResolver) boot(ctx context.Context, idxs []int, pw progress.Writer) ([]*client.Client, error) {
	clients := make([]*client.Client, len(idxs))

	baseCtx := ctx
	eg, ctx := errgroup.WithContext(ctx)

	for i, idx := range idxs {
		i, idx := i, idx
		eg.Go(func() error {
			c, err := r.clients.g.Do(ctx, fmt.Sprint(idx), func(ctx context.Context) (*client.Client, error) {
				if r.nodes[idx].Driver == nil {
					return nil, nil
				}
				r.clients.cacheMu.Lock()
				c, ok := r.clients.cache[idx]
				r.clients.cacheMu.Unlock()
				if ok {
					return c, nil
				}
				c, err := driver.Boot(ctx, baseCtx, r.nodes[idx].Driver, pw)
				if err != nil {
					return nil, err
				}
				r.clients.cacheMu.Lock()
				r.clients.cache[idx] = c
				r.clients.cacheMu.Unlock()
				return c, nil
			})
			if err != nil {
				return err
			}
			clients[i] = c
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return clients, nil
}

func (r *nodeResolver) opts(ctx context.Context, idxs []int, pw progress.Writer) ([]gateway.BuildOpts, error) {
	clients, err := r.boot(ctx, idxs, pw)
	if err != nil {
		return nil, err
	}

	bopts := make([]gateway.BuildOpts, len(clients))
	eg, ctx := errgroup.WithContext(ctx)
	for i, idxs := range idxs {
		i, idx := i, idxs
		c := clients[i]
		if c == nil {
			continue
		}
		eg.Go(func() error {
			opt, err := r.buildOpts.g.Do(ctx, fmt.Sprint(idx), func(ctx context.Context) (gateway.BuildOpts, error) {
				r.buildOpts.cacheMu.Lock()
				opt, ok := r.buildOpts.cache[idx]
				r.buildOpts.cacheMu.Unlock()
				if ok {
					return opt, nil
				}
				_, err := c.Build(ctx, client.SolveOpt{
					Internal: true,
				}, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
					opt = c.BuildOpts()
					return nil, nil
				}, nil)
				if err != nil {
					return gateway.BuildOpts{}, err
				}
				r.buildOpts.cacheMu.Lock()
				r.buildOpts.cache[idx] = opt
				r.buildOpts.cacheMu.Unlock()
				return opt, err
			})
			if err != nil {
				return err
			}
			bopts[i] = opt
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return bopts, nil
}

// recombineDriverPairs recombines resolved nodes that are on the same driver
// back together into a single node.
func recombineNodes(nodes []*resolvedNode) []*resolvedNode {
	result := make([]*resolvedNode, 0, len(nodes))
	lookup := map[int]int{}
	for _, node := range nodes {
		if idx, ok := lookup[node.driverIndex]; ok {
			result[idx].platforms = append(result[idx].platforms, node.platforms...)
		} else {
			lookup[node.driverIndex] = len(result)
			result = append(result, node)
		}
	}
	return result
}
