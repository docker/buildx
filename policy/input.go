package policy

import (
	"context"

	"github.com/containerd/platforms"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const maxMaterialDepth = 24

func SourceToInput(ctx context.Context, verifier PolicyVerifierProvider, src *gwpb.ResolveSourceMetaResponse, platform *ocispecs.Platform, logf func(logrus.Level, string)) (Input, error) {
	seen := map[string]struct{}{}
	return sourceToInputRecursive(ctx, verifier, src, platform, 0, seen, logf)
}

func sourceToInputRecursive(ctx context.Context, verifier PolicyVerifierProvider, src *gwpb.ResolveSourceMetaResponse, platform *ocispecs.Platform, depth int, seen map[string]struct{}, logf func(logrus.Level, string)) (Input, error) {
	if depth > maxMaterialDepth {
		return Input{}, errors.Errorf("provenance materials depth exceeds limit %d", maxMaterialDepth)
	}
	if src == nil || src.Source == nil {
		return Input{}, errors.New("source metadata response is required")
	}

	inp, unknowns, err := sourceToInput(ctx, verifier, src, platform, logf)
	if err != nil {
		return Input{}, err
	}
	inp.setUnknowns(unknowns)
	inp.Env.Depth = depth

	if inp.Image == nil || inp.Image.Provenance == nil || len(inp.Image.Provenance.materialsRaw) == 0 {
		return inp, nil
	}

	key := sourceUniqueIdentifier(src.Source, platform)
	if _, ok := seen[key]; ok {
		return Input{}, nil
	}
	seen[key] = struct{}{}
	defer delete(seen, key)

	materials := make([]Input, 0, len(inp.Image.Provenance.materialsRaw))
	for _, m := range inp.Image.Provenance.materialsRaw {
		matSrc, matPlatform, err := ParseSLSAMaterial(m)
		if err != nil {
			materials = append(materials, Input{})
			continue
		}
		if matSrc == nil {
			materials = append(materials, Input{})
			continue
		}
		matResp := &gwpb.ResolveSourceMetaResponse{Source: matSrc}
		child, err := sourceToInputRecursive(ctx, verifier, matResp, firstNonNilPlatform(matPlatform, platform), depth+1, seen, logf)
		if err != nil {
			return Input{}, errors.Wrapf(err, "failed to build material input for %q", m.URI)
		}
		materials = append(materials, child)
	}
	inp.Image.Provenance.Materials = materials
	return inp, nil
}

// sourceUniqueIdentifier is used only for recursive cycle detection safety.
func sourceUniqueIdentifier(src *pb.SourceOp, platform *ocispecs.Platform) string {
	if src == nil {
		return ""
	}
	key := src.Identifier
	if platform != nil {
		key += "|" + platforms.Format(*platform)
	}
	return key
}

func firstNonNilPlatform(values ...*ocispecs.Platform) *ocispecs.Platform {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}
