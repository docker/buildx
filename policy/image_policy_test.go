package policy

import (
	"context"
	"testing"

	"github.com/moby/buildkit/client/llb/sourceresolver"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	policyimage "github.com/moby/policy-helpers/image"
	policytypes "github.com/moby/policy-helpers/types"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

type fakeSourceResolver func(ctx context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error)

func (f fakeSourceResolver) ResolveSourceMetadata(ctx context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
	return f(ctx, op, opt)
}

// toSourceResolverChain converts a gateway attestation chain, as built by the
// shared test helpers, into the resolver-typed chain a BuildKit gateway
// returns.
func toSourceResolverChain(t *testing.T, ac *gwpb.AttestationChain) *sourceresolver.AttestationChain {
	t.Helper()
	out := &sourceresolver.AttestationChain{
		Root:                digest.Digest(ac.Root),
		ImageManifest:       digest.Digest(ac.ImageManifest),
		AttestationManifest: digest.Digest(ac.AttestationManifest),
		Blobs:               map[digest.Digest]sourceresolver.Blob{},
	}
	for _, sm := range ac.SignatureManifests {
		out.SignatureManifests = append(out.SignatureManifests, digest.Digest(sm))
	}
	for dgst, blob := range ac.Blobs {
		out.Blobs[digest.Digest(dgst)] = sourceresolver.Blob{
			Descriptor: ocispecs.Descriptor{
				MediaType: blob.Descriptor_.MediaType,
				Digest:    digest.Digest(blob.Descriptor_.Digest),
				Size:      blob.Descriptor_.Size,
			},
			Data: blob.Data,
		}
	}
	return out
}

func signedSigVerifier(sig *policytypes.SignatureInfo) PolicyVerifierProvider {
	return func() (PolicyVerifier, error) {
		return &mockPolicyVerifier{
			verifyImage: func(_ context.Context, _ policyimage.ReferrersProvider, _ ocispecs.Descriptor, _ *ocispecs.Platform) (*policytypes.SignatureInfo, error) {
				return sig, nil
			},
		}, nil
	}
}

func TestCheckSourceSigned(t *testing.T) {
	imgDigest := digest.FromString("resolved-image")
	var sawChainRequest bool

	resolver := fakeSourceResolver(func(_ context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
		require.Equal(t, "docker-image://docker.io/moby/buildkit:v0.31.2", op.Identifier)
		require.NotNil(t, opt.ImageOpt)
		if opt.ImageOpt.AttestationChain {
			sawChainRequest = true
		}
		return &sourceresolver.MetaResponse{
			Op: op,
			Image: &sourceresolver.ResolveImageResponse{
				Digest:           imgDigest,
				AttestationChain: toSourceResolverChain(t, newTestAttestationChain(t)),
			},
		}, nil
	})

	p := DefaultPolicy(Opt{VerifierProvider: signedSigVerifier(dockerGithubBuilderSig("moby/buildkit", "refs/tags/v0.31.2"))})
	dgst, err := p.CheckSource(context.Background(), "moby/buildkit:v0.31.2", &ocispecs.Platform{OS: "linux", Architecture: "amd64"}, resolver)
	require.NoError(t, err)
	require.Equal(t, imgDigest, dgst)
	require.True(t, sawChainRequest)
}

func TestCheckSourceDenied(t *testing.T) {
	newResolver := func(withSignatures bool) fakeSourceResolver {
		return func(_ context.Context, op *pb.SourceOp, _ sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
			ac := newTestAttestationChain(t)
			if !withSignatures {
				ac.SignatureManifests = nil
			}
			return &sourceresolver.MetaResponse{
				Op: op,
				Image: &sourceresolver.ResolveImageResponse{
					Digest:           digest.FromString("resolved-image"),
					AttestationChain: toSourceResolverChain(t, ac),
				},
			}, nil
		}
	}

	t.Run("unsigned-image", func(t *testing.T) {
		p := DefaultPolicy(Opt{VerifierProvider: signedSigVerifier(dockerGithubBuilderSig("moby/buildkit", "refs/tags/v0.31.2"))})
		_, err := p.CheckSource(context.Background(), "moby/buildkit:v0.31.2", &ocispecs.Platform{OS: "linux", Architecture: "amd64"}, newResolver(false))
		var verr *ImageVerificationError
		require.ErrorAs(t, err, &verr)
		require.ErrorContains(t, err, "signature is required for v0.31.2 tag")
	})

	t.Run("signature-ref-mismatch", func(t *testing.T) {
		p := DefaultPolicy(Opt{VerifierProvider: signedSigVerifier(dockerGithubBuilderSig("moby/buildkit", "refs/tags/v0.30.0"))})
		_, err := p.CheckSource(context.Background(), "moby/buildkit:v0.31.2", &ocispecs.Platform{OS: "linux", Architecture: "amd64"}, newResolver(true))
		var verr *ImageVerificationError
		require.ErrorAs(t, err, &verr)
	})

	t.Run("floating-tag-without-signature", func(t *testing.T) {
		p := DefaultPolicy(Opt{VerifierProvider: signedSigVerifier(dockerGithubBuilderSig("moby/buildkit", "refs/tags/v0.31.2"))})
		_, err := p.CheckSource(context.Background(), "moby/buildkit:buildx-stable-1", &ocispecs.Platform{OS: "linux", Architecture: "amd64"}, newResolver(false))
		var verr *ImageVerificationError
		require.ErrorAs(t, err, &verr)
		require.ErrorContains(t, err, "signature is required for buildx-stable-1 tag")
	})
}

func TestCheckSourceOldTagPinnedWithoutMetadata(t *testing.T) {
	// Releases before signing do not require metadata for the decision, but
	// the digest is still resolved so the caller can pin the image.
	imgDigest := digest.FromString("old-release")
	var calls int

	resolver := fakeSourceResolver(func(_ context.Context, op *pb.SourceOp, opt sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
		calls++
		require.NotNil(t, opt.ImageOpt)
		require.False(t, opt.ImageOpt.AttestationChain)
		require.True(t, opt.ImageOpt.NoConfig)
		return &sourceresolver.MetaResponse{
			Op:    op,
			Image: &sourceresolver.ResolveImageResponse{Digest: imgDigest},
		}, nil
	})

	p := DefaultPolicy(Opt{})
	dgst, err := p.CheckSource(context.Background(), "moby/buildkit:v0.26.2", nil, resolver)
	require.NoError(t, err)
	require.Equal(t, imgDigest, dgst)
	require.Equal(t, 1, calls)
}

func TestCheckSourceCanonicalDigestWithoutResolution(t *testing.T) {
	// An untagged canonical reference passes the policy and pins to its own
	// digest without any metadata resolution.
	dgst := digest.FromString("pinned")
	resolver := fakeSourceResolver(func(context.Context, *pb.SourceOp, sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
		return nil, errors.New("unexpected resolution")
	})

	p := DefaultPolicy(Opt{})
	out, err := p.CheckSource(context.Background(), "moby/buildkit@"+dgst.String(), nil, resolver)
	require.NoError(t, err)
	require.Equal(t, dgst, out)
}

func TestCheckSourceMissingChainSupport(t *testing.T) {
	// A daemon that cannot resolve the attestation chain (or an image
	// without one) must produce a clear failure instead of looping.
	resolver := fakeSourceResolver(func(_ context.Context, op *pb.SourceOp, _ sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
		return &sourceresolver.MetaResponse{
			Op:    op,
			Image: &sourceresolver.ResolveImageResponse{Digest: digest.FromString("resolved-image")},
		}, nil
	})

	p := DefaultPolicy(Opt{})
	_, err := p.CheckSource(context.Background(), "moby/buildkit:buildx-stable-1", nil, resolver)
	require.ErrorContains(t, err, "no signature metadata available")
}

func TestCheckSourceUnsupportedScheme(t *testing.T) {
	_, err := DefaultPolicy(Opt{}).CheckSource(context.Background(), "git://github.com/moby/buildkit.git", nil, nil)
	require.ErrorContains(t, err, "unsupported source")
}
