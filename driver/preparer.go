package driver

import (
	"context"

	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type PrepareBuildOptions struct {
	SolveOpt             *client.SolveOpt
	Docker               *dockerutil.Client
	Tags                 []string
	Platforms            []ocispecs.Platform
	MultiDriver          bool
	NoOutput             bool // prevents drivers from adding output for metadata-only requests
	BuildInfoAttrs       string
	BuildInfoAttrsSet    bool
	NoDefaultOCIArtifact bool
	Progress             progress.Writer
}

type BuildPreparer interface {
	PrepareBuild(ctx context.Context, opts *PrepareBuildOptions) error
}
