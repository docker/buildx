package build

import (
	"context"
	stderrors "errors"
	"net"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/progress"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

func Dial(ctx context.Context, nodes []builder.Node, pw progress.Writer, platform *v1.Platform) (net.Conn, error) {
	nodes, err := filterAvailableNodes(nodes)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return nil, errors.New("no nodes available")
	}

	var pls []v1.Platform
	if platform != nil {
		pls = []v1.Platform{*platform}
	}

	opts := map[string]Options{"default": {Platforms: pls}}
	resolved, err := resolveDrivers(ctx, nodes, opts, pw)
	if err != nil {
		return nil, err
	}

	var dialError error
	for _, ls := range resolved {
		for _, rn := range ls {
			if platform != nil {
				p := *platform
				var found bool
				for _, pp := range rn.platforms {
					if platforms.Only(p).Match(pp) {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}

			conn, err := nodes[rn.driverIndex].Driver.Dial(ctx)
			if err == nil {
				return conn, nil
			}
			dialError = stderrors.Join(err)
		}
	}

	return nil, errors.Wrap(dialError, "no nodes available")
}
