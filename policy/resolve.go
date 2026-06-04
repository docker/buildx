package policy

import (
	"context"
	"slices"
	"strings"

	"github.com/docker/buildx/util/sourcemeta"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type SourceMetadataResolver interface {
	ResolveSourceMetadata(context.Context, *pb.SourceOp, sourceresolver.Opt) (*sourceresolver.MetaResponse, error)
}

func ResolveInputUnknowns(ctx context.Context, input *Input, rootSource *pb.SourceOp, unknowns []string, rootPlatform *pb.Platform, defaultPlatform *ocispecs.Platform, resolver SourceMetadataResolver, verifier PolicyVerifierProvider, logf func(logrus.Level, string)) (bool, *gwpb.ResolveSourceMetaRequest, error) {
	if input == nil || len(unknowns) == 0 {
		return false, nil, nil
	}
	return resolveNodeUnknowns(ctx, input, rootSource, defaultPlatform, normalizeNodeUnknowns(unknowns), rootPlatform, defaultPlatform, resolver, verifier, logf, nil)
}

func resolveNodeUnknowns(ctx context.Context, node *Input, source *pb.SourceOp, nodePlatform *ocispecs.Platform, unknowns []string, rootPlatform *pb.Platform, defaultPlatform *ocispecs.Platform, resolver SourceMetadataResolver, verifier PolicyVerifierProvider, logf func(logrus.Level, string), setNode func(Input) error) (bool, *gwpb.ResolveSourceMetaRequest, error) {
	directUnknowns, childUnknowns := splitNodeUnknowns(unknowns)
	if len(directUnknowns) > 0 {
		req, err := sourceResolveRequest(source, nodePlatform, setNode == nil, directUnknowns, rootPlatform, logf)
		if err != nil {
			return false, nil, err
		}
		if req != nil {
			if setNode == nil {
				return false, req, nil
			}
			if resolver == nil {
				return false, nil, errors.Errorf("material metadata resolution requires source resolver")
			}
			resp, err := resolveSourceMetaWithResolver(ctx, resolver, req, defaultPlatform)
			if err != nil {
				return false, nil, errors.Wrap(err, "failed to resolve source metadata for material")
			}
			nextInput, err := SourceToInput(ctx, verifier, resp, nodePlatform, logf)
			if err != nil {
				return false, nil, errors.Wrap(err, "failed to rebuild material input")
			}
			if err := setNode(nextInput); err != nil {
				return false, nil, err
			}
			return true, nil, nil
		}
	}

	for idx, childNodeUnknowns := range childUnknowns {
		if node == nil || node.Image == nil || node.Image.Provenance == nil {
			continue
		}
		if idx < 0 || idx >= len(node.Image.Provenance.Materials) || idx >= len(node.Image.Provenance.materialsRaw) {
			continue
		}
		raw := node.Image.Provenance.materialsRaw[idx]
		childSource, childNodePlatform, err := ParseSLSAMaterial(raw)
		if err != nil {
			continue
		}
		childPlatform := firstNonNilPlatform(childNodePlatform, nodePlatform)
		retry, next, err := resolveNodeUnknowns(ctx, &node.Image.Provenance.Materials[idx], childSource, childPlatform, childNodeUnknowns, rootPlatform, defaultPlatform, resolver, verifier, logf, func(next Input) error {
			node.Image.Provenance.Materials[idx] = next
			return nil
		})
		if err != nil {
			return false, nil, err
		}
		if retry || next != nil {
			return retry, next, nil
		}
	}
	return false, nil, nil
}

func splitNodeUnknowns(unknowns []string) ([]string, map[int][]string) {
	child := map[int][]string{}
	var direct []string
	for _, u := range unknowns {
		if u == "" {
			continue
		}
		if idx, rest, ok := isMaterialKey(u); ok {
			if rest == "" {
				continue
			}
			if !slices.Contains(child[idx], rest) {
				child[idx] = append(child[idx], rest)
			}
			continue
		}
		if !slices.Contains(direct, u) {
			direct = append(direct, u)
		}
	}
	return direct, child
}

func normalizeNodeUnknowns(unknowns []string) []string {
	out := make([]string, 0, len(unknowns))
	for _, u := range unknowns {
		v := strings.TrimPrefix(u, "input.")
		if v == "" {
			continue
		}
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func sourceResolveRequest(source *pb.SourceOp, nodePlatform *ocispecs.Platform, rootNode bool, fields []string, rootPlatform *pb.Platform, logf func(logrus.Level, string)) (*gwpb.ResolveSourceMetaRequest, error) {
	if source == nil {
		return nil, nil
	}
	req := &gwpb.ResolveSourceMetaRequest{Source: source}
	if nodePlatform != nil {
		req.Platform = &pb.Platform{OS: nodePlatform.OS, Architecture: nodePlatform.Architecture, Variant: nodePlatform.Variant}
	} else if rootNode {
		req.Platform = rootPlatform
	}
	if err := AddUnknownsWithLogger(logf, req, fields); err != nil {
		return nil, err
	}
	if req.Image == nil && req.Git == nil && !hasHTTPUnknowns(fields) {
		return nil, nil
	}
	return req, nil
}

func resolveSourceMetaWithResolver(ctx context.Context, resolver SourceMetadataResolver, req *gwpb.ResolveSourceMetaRequest, defaultPlatform *ocispecs.Platform) (*gwpb.ResolveSourceMetaResponse, error) {
	if resolver == nil {
		return nil, errors.New("source resolver is not configured")
	}
	if req == nil || req.Source == nil {
		return nil, errors.New("source metadata request is missing source")
	}
	resp, err := resolver.ResolveSourceMetadata(ctx, req.Source, sourcemeta.ToResolverOpt(req, defaultPlatform))
	if err != nil {
		return nil, err
	}
	return sourcemeta.ToGatewayMetaResponse(resp), nil
}
