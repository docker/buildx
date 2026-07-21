package policy

import (
	"context"
	"strings"

	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/buildx/util/sourcemeta"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// ImageVerificationError is returned by CheckSource when the policy denied
// the image. Any other error from CheckSource means the policy could not be
// evaluated at all (e.g. source metadata could not be resolved).
type ImageVerificationError struct {
	Ref      string
	Messages []string
}

func (e *ImageVerificationError) Error() string {
	if len(e.Messages) == 0 {
		return "image " + e.Ref + " is not allowed by policy"
	}
	return strings.Join(e.Messages, "; ")
}

// CheckSource evaluates the policy against a source reference and returns
// the digest it resolved to. Source metadata, including the signature
// attestation chain, is resolved through resolver, usually a BuildKit
// gateway (see sourcemeta.NewResolver). Only docker-image sources are
// supported: source is either a plain image reference or a docker-image://
// identifier.
func (p *Policy) CheckSource(ctx context.Context, source string, platform *ocispecs.Platform, resolver SourceMetadataResolver) (digest.Digest, error) {
	refstr, ok := strings.CutPrefix(source, "docker-image://")
	if !ok {
		if strings.Contains(source, "://") {
			return "", errors.Errorf("unsupported source %s for policy evaluation", source)
		}
		refstr = source
	}
	named, err := reference.ParseNormalizedNamed(refstr)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse image reference %s", refstr)
	}
	named = reference.TagNameOnly(named)

	if platform == nil {
		pl := platforms.Normalize(platforms.DefaultSpec())
		platform = &pl
	}

	srcOp := &pb.SourceOp{Identifier: "docker-image://" + named.String()}
	src := &gwpb.ResolveSourceMetaResponse{Source: srcOp}

	for range maxResolveIterations {
		decision, next, err := p.CheckPolicy(ctx, &policysession.CheckPolicyRequest{
			Platform: toPBPlatform(platform),
			Source:   src,
		})
		if err != nil {
			return "", err
		}
		if next != nil {
			src, err = resolveSourceMeta(ctx, resolver, next, srcOp, platform)
			if err != nil {
				return "", err
			}
			if next.Image != nil && next.Image.AttestationChain && (src.Image == nil || src.Image.AttestationChain == nil) {
				return "", errors.Errorf("no signature metadata available for %s: image is not signed or the daemon does not support resolving it", named.String())
			}
			continue
		}
		if decision == nil {
			return "", errors.New("policy returned no decision")
		}
		switch decision.Action {
		case spb.PolicyAction_ALLOW, spb.PolicyAction_CONVERT:
			return sourceDigest(ctx, resolver, src, srcOp, named, platform)
		case spb.PolicyAction_DENY:
			msgs := make([]string, 0, len(decision.DenyMessages))
			for _, m := range decision.DenyMessages {
				if m != nil && m.Message != "" {
					msgs = append(msgs, m.Message)
				}
			}
			return "", errors.WithStack(&ImageVerificationError{Ref: named.String(), Messages: msgs})
		default:
			return "", errors.Errorf("unknown policy action %s", decision.Action)
		}
	}
	return "", errors.New("maximum attempts reached for resolving image metadata")
}

func resolveSourceMeta(ctx context.Context, resolver SourceMetadataResolver, req *gwpb.ResolveSourceMetaRequest, srcOp *pb.SourceOp, platform *ocispecs.Platform) (*gwpb.ResolveSourceMetaResponse, error) {
	if resolver == nil {
		return nil, errors.New("source metadata resolver is required for policy evaluation")
	}
	target := srcOp
	if req.Source != nil {
		target = req.Source
	}
	resp, err := resolver.ResolveSourceMetadata(ctx, target, sourcemeta.ToResolverOpt(req, platform))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve source metadata for %s", target.Identifier)
	}
	out := sourcemeta.ToGatewayMetaResponse(resp)
	if out.Source == nil {
		out.Source = target
	}
	return out, nil
}

// sourceDigest returns the digest the allowed source resolved to so callers
// can pin it. When the policy decided without loading image metadata the
// digest comes from the reference itself or from one extra resolution.
func sourceDigest(ctx context.Context, resolver SourceMetadataResolver, src *gwpb.ResolveSourceMetaResponse, srcOp *pb.SourceOp, named reference.Named, platform *ocispecs.Platform) (digest.Digest, error) {
	if src.Image != nil && src.Image.Digest != "" {
		return digest.Digest(src.Image.Digest), nil
	}
	if canonical, ok := named.(reference.Canonical); ok {
		return canonical.Digest(), nil
	}
	resp, err := resolveSourceMeta(ctx, resolver, &gwpb.ResolveSourceMetaRequest{
		Source: srcOp,
		Image:  &gwpb.ResolveSourceImageRequest{NoConfig: true},
	}, srcOp, platform)
	if err != nil {
		return "", err
	}
	if resp.Image == nil || resp.Image.Digest == "" {
		return "", errors.Errorf("failed to resolve digest for %s", named.String())
	}
	return digest.Digest(resp.Image.Digest), nil
}

func toPBPlatform(p *ocispecs.Platform) *pb.Platform {
	if p == nil {
		return nil
	}
	return &pb.Platform{
		OS:           p.OS,
		Architecture: p.Architecture,
		Variant:      p.Variant,
		OSVersion:    p.OSVersion,
		OSFeatures:   p.OSFeatures,
	}
}
