package build

import (
	"context"
	"os"
	"path/filepath"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	gwclient "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/pkg/errors"
)

const maxDockerfileSize = 2 * 1024 * 1024 // 2 MB

func createTempDockerfileFromURL(ctx context.Context, d *driver.DriverHandle, url string, pw progress.Writer) (string, error) {
	c, err := driver.Boot(ctx, ctx, d, pw)
	if err != nil {
		return "", err
	}
	var out string
	ch, done := progress.NewChannel(pw)
	defer func() { <-done }()
	_, err = c.Build(ctx, client.SolveOpt{Internal: true}, "buildx", func(ctx context.Context, c gwclient.Client) (*gwclient.Result, error) {
		def, err := llb.HTTP(url, llb.Filename("Dockerfile"), llb.WithCustomNamef("[internal] load %s", url)).Marshal(ctx)
		if err != nil {
			return nil, err
		}

		res, err := c.Solve(ctx, gwclient.SolveRequest{
			Definition: def.ToPB(),
		})
		if err != nil {
			return nil, err
		}
		ref, err := res.SingleRef()
		if err != nil {
			return nil, err
		}
		stat, err := ref.StatFile(ctx, gwclient.StatRequest{
			Path: "Dockerfile",
		})
		if err != nil {
			return nil, err
		}
		if stat.Size > maxDockerfileSize {
			return nil, errors.Errorf("Dockerfile %s bigger than allowed max size (%s)", url, units.HumanSize(maxDockerfileSize))
		}

		dt, err := ref.ReadFile(ctx, gwclient.ReadRequest{
			Filename: "Dockerfile",
		})
		if err != nil {
			return nil, err
		}
		dir, err := os.MkdirTemp("", "buildx")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), dt, 0600); err != nil {
			return nil, err
		}
		out = dir
		return nil, nil
	}, ch)
	if err != nil {
		return "", err
	}
	return out, nil
}
