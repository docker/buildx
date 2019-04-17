package platformutil

import (
	"strings"

	"github.com/containerd/containerd/platforms"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

func Parse(platformsStr []string) ([]specs.Platform, error) {
	if len(platformsStr) == 0 {
		return nil, nil
	}
	out := make([]specs.Platform, 0, len(platformsStr))
	for _, s := range platformsStr {
		parts := strings.Split(s, ",")
		if len(parts) > 1 {
			p, err := Parse(parts)
			if err != nil {
				return nil, err
			}
			out = append(out, p...)
			continue
		}
		p, err := platforms.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, platforms.Normalize(p))
	}
	return out, nil
}
