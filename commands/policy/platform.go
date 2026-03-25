package policy

import (
	"github.com/containerd/platforms"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

func parsePlatform(platform string) (*ocispecs.Platform, error) {
	p, err := platforms.Parse(platform)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid platform %q", platform)
	}
	p = platforms.Normalize(p)
	return &p, nil
}

func toPBPlatform(platform ocispecs.Platform) *pb.Platform {
	return &pb.Platform{
		Architecture: platform.Architecture,
		OS:           platform.OS,
		Variant:      platform.Variant,
	}
}
