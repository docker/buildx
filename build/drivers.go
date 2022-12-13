package build

import (
	"context"
	_ "crypto/sha256" // ensure digests can be computed

	"github.com/containerd/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/options"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/util/tracing"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

type driverPair struct {
	driverIndex int
	platforms   []specs.Platform
	so          *client.SolveOpt
	bopts       gateway.BuildOpts
}

func driverIndexes(m map[string][]driverPair) []int {
	out := make([]int, 0, len(m))
	visited := map[int]struct{}{}
	for _, dp := range m {
		for _, d := range dp {
			if _, ok := visited[d.driverIndex]; ok {
				continue
			}
			visited[d.driverIndex] = struct{}{}
			out = append(out, d.driverIndex)
		}
	}
	return out
}

func allIndexes(l int) []int {
	out := make([]int, 0, l)
	for i := 0; i < l; i++ {
		out = append(out, i)
	}
	return out
}

func ensureBooted(ctx context.Context, nodes []builder.Node, idxs []int, pw progress.Writer) ([]*client.Client, error) {
	clients := make([]*client.Client, len(nodes))

	baseCtx := ctx
	eg, ctx := errgroup.WithContext(ctx)

	for _, i := range idxs {
		func(i int) {
			eg.Go(func() error {
				c, err := driver.Boot(ctx, baseCtx, nodes[i].Driver, pw)
				if err != nil {
					return err
				}
				clients[i] = c
				return nil
			})
		}(i)
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return clients, nil
}

func splitToDriverPairs(availablePlatforms map[string]int, opt map[string]options.Options) map[string][]driverPair {
	m := map[string][]driverPair{}
	for k, opt := range opt {
		mm := map[int][]specs.Platform{}
		for _, p := range opt.Platforms {
			k := platforms.Format(p)
			idx := availablePlatforms[k] // default 0
			pp := mm[idx]
			pp = append(pp, p)
			mm[idx] = pp
		}
		// if no platform is specified, use first driver
		if len(mm) == 0 {
			mm[0] = nil
		}
		dps := make([]driverPair, 0, 2)
		for idx, pp := range mm {
			dps = append(dps, driverPair{driverIndex: idx, platforms: pp})
		}
		m[k] = dps
	}
	return m
}

func resolveDrivers(ctx context.Context, nodes []builder.Node, opt map[string]options.Options, pw progress.Writer) (map[string][]driverPair, []*client.Client, error) {
	dps, clients, err := resolveDriversBase(ctx, nodes, opt, pw)
	if err != nil {
		return nil, nil, err
	}

	bopts := make([]gateway.BuildOpts, len(clients))

	span, ctx := tracing.StartSpan(ctx, "load buildkit capabilities", trace.WithSpanKind(trace.SpanKindInternal))

	eg, ctx := errgroup.WithContext(ctx)
	for i, c := range clients {
		if c == nil {
			continue
		}

		func(i int, c *client.Client) {
			eg.Go(func() error {
				clients[i].Build(ctx, client.SolveOpt{}, "buildx", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
					bopts[i] = c.BuildOpts()
					return nil, nil
				}, nil)
				return nil
			})
		}(i, c)
	}

	err = eg.Wait()
	tracing.FinishWithError(span, err)
	if err != nil {
		return nil, nil, err
	}
	for key := range dps {
		for i, dp := range dps[key] {
			dps[key][i].bopts = bopts[dp.driverIndex]
		}
	}

	return dps, clients, nil
}

func resolveDriversBase(ctx context.Context, nodes []builder.Node, opt map[string]options.Options, pw progress.Writer) (map[string][]driverPair, []*client.Client, error) {
	availablePlatforms := map[string]int{}
	for i, node := range nodes {
		for _, p := range node.Platforms {
			availablePlatforms[platforms.Format(p)] = i
		}
	}

	undetectedPlatform := false
	allPlatforms := map[string]int{}
	for _, opt := range opt {
		for _, p := range opt.Platforms {
			k := platforms.Format(p)
			allPlatforms[k] = -1
			if _, ok := availablePlatforms[k]; !ok {
				undetectedPlatform = true
			}
		}
	}

	// fast path
	if len(nodes) == 1 || len(allPlatforms) == 0 {
		m := map[string][]driverPair{}
		for k, opt := range opt {
			m[k] = []driverPair{{driverIndex: 0, platforms: opt.Platforms}}
		}
		clients, err := ensureBooted(ctx, nodes, driverIndexes(m), pw)
		if err != nil {
			return nil, nil, err
		}
		return m, clients, nil
	}

	// map based on existing platforms
	if !undetectedPlatform {
		m := splitToDriverPairs(availablePlatforms, opt)
		clients, err := ensureBooted(ctx, nodes, driverIndexes(m), pw)
		if err != nil {
			return nil, nil, err
		}
		return m, clients, nil
	}

	// boot all drivers in k
	clients, err := ensureBooted(ctx, nodes, allIndexes(len(nodes)), pw)
	if err != nil {
		return nil, nil, err
	}

	eg, ctx := errgroup.WithContext(ctx)
	workers := make([][]*client.WorkerInfo, len(clients))

	for i, c := range clients {
		if c == nil {
			continue
		}
		func(i int) {
			eg.Go(func() error {
				ww, err := clients[i].ListWorkers(ctx)
				if err != nil {
					return errors.Wrap(err, "listing workers")
				}
				workers[i] = ww
				return nil
			})

		}(i)
	}

	if err := eg.Wait(); err != nil {
		return nil, nil, err
	}

	for i, ww := range workers {
		for _, w := range ww {
			for _, p := range w.Platforms {
				p = platforms.Normalize(p)
				ps := platforms.Format(p)

				if _, ok := availablePlatforms[ps]; !ok {
					availablePlatforms[ps] = i
				}
			}
		}
	}

	return splitToDriverPairs(availablePlatforms, opt), clients, nil
}
