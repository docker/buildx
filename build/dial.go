package build

import (
	"context"
	stderrors "errors"
	"net"
	"slices"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/build/resolver"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/progress"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

func Dial(ctx context.Context, nodes []builder.Node, pw progress.Writer, platform *ocispecs.Platform) (net.Conn, error) {
	nodes, err := filterAvailableNodes(nodes)
	if err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return nil, errors.New("no nodes available")
	}

	var pls []ocispecs.Platform
	if platform != nil {
		pls = []ocispecs.Platform{*platform}
	}

	resolved, err := resolver.Resolve(ctx, nodes, pls, pw)
	if err != nil {
		return nil, err
	}

	var dialError error
	for _, rnode := range resolved {
		if platform != nil {
			if !slices.ContainsFunc(rnode.Platforms(), platforms.Only(*platform).Match) {
				continue
			}
		}

		driver := rnode.Node().Driver
		conn, err := driver.Dial(ctx)
		if err == nil {
			return conn, nil
		}
		dialError = stderrors.Join(err)
	}

	return nil, errors.Wrap(dialError, "no nodes available")
}
