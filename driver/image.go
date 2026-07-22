package driver

import (
	"context"

	"github.com/distribution/reference"
	"github.com/docker/buildx/driver/bkimage"
	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/progress"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// VerifyImageRef evaluates ref against the builtin default policy through
// verify and returns the reference pinned to the digest that verification
// resolved. The boolean result reports whether the policy applied: it is
// false, with ref returned unchanged, when there is no verifier, when ref is
// pinned by digest without a tag, or when ref is outside the managed
// moby/buildkit repository (unmanaged images pass through the default policy
// unchanged). A tagged canonical reference is still verified because its tag
// carries the release identity checked by the policy.
func VerifyImageRef(ctx context.Context, l progress.SubLogger, ref string, platform *ocispecs.Platform, resolver policy.SourceMetadataResolver, verify ImageVerifier) (string, bool, error) {
	if verify == nil {
		return ref, false, nil
	}
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return "", false, errors.Wrapf(err, "failed to parse image reference %s", ref)
	}
	_, isCanonical := named.(reference.Canonical)
	_, isTagged := named.(reference.Tagged)
	if isCanonical && !isTagged {
		return ref, false, nil
	}
	if named.Name() != bkimage.TrustedRepo {
		return ref, false, nil
	}
	named = reference.TagNameOnly(named)

	var dgst digest.Digest
	if err := l.Wrap("verifying image "+named.String(), func() error {
		var err error
		dgst, err = verify(ctx, named.String(), platform, resolver)
		return err
	}); err != nil {
		return "", true, err
	}
	canonical, err := reference.WithDigest(named, dgst)
	if err != nil {
		return "", true, errors.Wrapf(err, "failed to construct canonical reference for %s", ref)
	}
	return canonical.String(), true, nil
}
