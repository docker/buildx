package policy

import (
	"context"
	"testing"
	"time"

	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	moby_buildkit_v1_sourcepolicy "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	policyimage "github.com/moby/policy-helpers/image"
	policytypes "github.com/moby/policy-helpers/types"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// makeDefaultPolicy returns a Policy instance backed by the embedded
// default policy module, optionally wired to a mock signature verifier
// returning the supplied SignatureInfo for image attestations.
func makeDefaultPolicy(t *testing.T, sigInfo *policytypes.SignatureInfo) *Policy {
	t.Helper()

	var verifierProvider PolicyVerifierProvider
	if sigInfo != nil {
		verifierProvider = func() (PolicyVerifier, error) {
			return &mockPolicyVerifier{
				verifyImage: func(_ context.Context, _ policyimage.ReferrersProvider, _ ocispecs.Descriptor, _ *ocispecs.Platform) (*policytypes.SignatureInfo, error) {
					return sigInfo, nil
				},
			}, nil
		}
	}
	return NewPolicy(Opt{
		Files: []File{{
			Filename: DefaultPolicyFilename,
			Data:     DefaultPolicyData(),
		}},
		Log: func(level logrus.Level, msg string) {
			t.Logf("[%s] %s", level, msg)
		},
		VerifierProvider: verifierProvider,
	})
}

// dockerGithubBuilderSig returns a SignatureInfo that satisfies the
// docker_github_builder_signature helper for the given source repository
// and ref. Pass an empty ref to omit the SourceRepositoryRef field.
func dockerGithubBuilderSig(sourceRepo, sourceRef string) *policytypes.SignatureInfo {
	return &policytypes.SignatureInfo{
		Kind:          policytypes.KindDockerGithubBuilder,
		SignatureType: policytypes.SignatureBundleV03,
		Timestamps: []policytypes.TimestampVerificationResult{
			{Type: "rekor", URI: "https://rekor.sigstore.dev", Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		Signer: &certificate.Summary{
			CertificateIssuer: "CN=sigstore-intermediate,O=sigstore.dev",
			Extensions: certificate.Extensions{
				Issuer:              "https://token.actions.githubusercontent.com",
				SourceRepositoryURI: "https://github.com/" + sourceRepo,
				SourceRepositoryRef: sourceRef,
				RunnerEnvironment:   "github-hosted",
			},
		},
	}
}

// runDefaultPolicyImage evaluates the default policy against the given image
// reference. An attestation chain and an empty image config are always
// supplied so that the policy can fully resolve metadata and produce a
// decision (rather than requesting more data via the next response).
func runDefaultPolicyImage(t *testing.T, p *Policy, ref string) *policysession.DecisionResponse {
	t.Helper()
	src := &gwpb.ResolveSourceMetaResponse{
		Source: &pb.SourceOp{Identifier: "docker-image://" + ref},
		Image: &gwpb.ResolveSourceImageResponse{
			Digest:           "sha256:abababababababababababababababababababababababababababababababab",
			Config:           []byte(`{"created":"2024-01-01T00:00:00Z","config":{}}`),
			AttestationChain: newTestAttestationChain(t),
		},
	}
	resp, _, err := p.CheckPolicy(context.Background(), &policysession.CheckPolicyRequest{
		Platform: &pb.Platform{OS: "linux", Architecture: "amd64"},
		Source:   src,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp
}

func TestDefaultPolicyAllowsNonImageSources(t *testing.T) {
	p := makeDefaultPolicy(t, nil)
	src := &gwpb.ResolveSourceMetaResponse{
		Source: &pb.SourceOp{Identifier: "https://example.com/foo.tar.gz"},
		HTTP: &gwpb.ResolveSourceHTTPResponse{
			Checksum: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}
	resp, _, err := p.CheckPolicy(context.Background(), &policysession.CheckPolicyRequest{
		Source: src,
	})
	require.NoError(t, err)
	require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW, resp.Action)
}

func TestDefaultPolicyImages(t *testing.T) {
	testCases := []struct {
		name    string
		sig     *policytypes.SignatureInfo
		ref     string
		allow   bool
		denyMsg string
	}{
		{
			name:  "allows_unrelated_images",
			ref:   "alpine:latest",
			allow: true,
		},
		{
			name:  "dockerfile_digest_only_always_allowed",
			ref:   "docker/dockerfile@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			allow: true,
		},
		{
			name:    "dockerfile_tagged_digest_denied",
			ref:     "docker/dockerfile:1.21.0@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:  "dockerfile_old_version_allowed_unsigned",
			ref:   "docker/dockerfile:1.20.0",
			allow: true,
		},
		{
			name:    "dockerfile_new_version_requires_signature",
			ref:     "docker/dockerfile:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:    "dockerfile_new_minor_version_requires_signature",
			ref:     "docker/dockerfile:1.21",
			denyMsg: "signature is required for 1.21 tag",
		},
		{
			name:    "dockerfile_new_major_version_requires_signature",
			ref:     "docker/dockerfile:1",
			denyMsg: "signature is required for 1 tag",
		},
		{
			name:  "dockerfile_new_version_allowed_with_matching_signature",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0"),
			ref:   "docker/dockerfile:1.21.0",
			allow: true,
		},
		{
			name:  "dockerfile_new_minor_version_allowed_with_matching_patch_signature",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0"),
			ref:   "docker/dockerfile:1.21",
			allow: true,
		},
		{
			name:  "dockerfile_new_version_allowed_with_matching_signature_labs",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0-labs"),
			ref:   "docker/dockerfile:1.21.0-labs",
			allow: true,
		},
		{
			name:  "dockerfile_new_version_allowed_with_matching_signature_labs2",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0-labs"),
			ref:   "docker/dockerfile:1.21-labs",
			allow: true,
		},
		{
			name:  "dockerfile_new_version_allowed_with_matching_signature_labs3",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0-labs"),
			ref:   "docker/dockerfile:1-labs",
			allow: true,
		},
		{
			name:  "dockerfile_new_version_allowed_with_matching_signature_labs4",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0-labs"),
			ref:   "docker/dockerfile:labs",
			allow: true,
		},
		{
			name:    "dockerfile_new_version_denied_with_nonlabs_signature_for_labs_tag",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0"),
			ref:     "docker/dockerfile:1-labs",
			denyMsg: "signature is required for 1-labs tag",
		},
		{
			name:    "dockerfile_new_version_denied_with_mismatched_ref_labs",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.22.0-labs"),
			ref:     "docker/dockerfile:1.21.0-labs",
			denyMsg: "signature is required for 1.21.0-labs tag",
		},
		{
			name:    "dockerfile_new_version_denied_with_nonlabs_signature_for_exact_labs_tag",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0"),
			ref:     "docker/dockerfile:1.21.0-labs",
			denyMsg: "signature is required for 1.21.0-labs tag",
		},
		{
			name:    "dockerfile_new_version_denied_with_wrong_signature_repo",
			sig:     dockerGithubBuilderSig("docker/dockerfile", "refs/tags/dockerfile/1.21.0"),
			ref:     "docker/dockerfile:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:    "dockerfile_upstream_new_version_denied_with_wrong_signature_repo",
			sig:     dockerGithubBuilderSig("docker/dockerfile-upstream", "refs/tags/dockerfile/1.21.0"),
			ref:     "docker/dockerfile-upstream:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:    "dockerfile_upstream_new_version_requires_signature",
			ref:     "docker/dockerfile-upstream:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:  "dockerfile_upstream_new_version_allowed_with_matching_signature",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.21.0"),
			ref:   "docker/dockerfile-upstream:1.21.0",
			allow: true,
		},
		{
			name:    "dockerfile_upstream_new_version_denied_with_mismatched_ref",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.22.0"),
			ref:     "docker/dockerfile-upstream:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:    "dockerfile_new_version_denied_with_mismatched_ref",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.22.0"),
			ref:     "docker/dockerfile:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:    "dockerfile_new_minor_version_denied_with_newer_patch_ref",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.23.0"),
			ref:     "docker/dockerfile:1.22",
			denyMsg: "signature is required for 1.22 tag",
		},
		{
			name:    "dockerfile_new_version_denied_with_mismatched_ref_labs",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.22.0-labs"),
			ref:     "docker/dockerfile:1.21.0-labs",
			denyMsg: "signature is required for 1.21.0-labs tag",
		},
		{
			name:    "dockerfile_new_version_denied_with_wrong_signature_tag",
			sig:     dockerGithubBuilderSig("moby/buildkit", "refs/tags/1.21.0"),
			ref:     "docker/dockerfile:1.21.0",
			denyMsg: "signature is required for 1.21.0 tag",
		},
		{
			name:  "dockerfile_latest_allowed_with_signature_any_ref",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.30.0"),
			ref:   "docker/dockerfile:latest",
			allow: true,
		},
		{
			name:    "dockerfile_latest_denied_without_signature",
			ref:     "docker/dockerfile:latest",
			denyMsg: "signature is required for latest tag",
		},
		{
			name:  "dockerfile_upstream_latest_allowed_with_signature_any_ref",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/tags/dockerfile/1.30.0"),
			ref:   "docker/dockerfile-upstream:latest",
			allow: true,
		},
		{
			name:    "dockerfile_upstream_latest_denied_without_signature",
			ref:     "docker/dockerfile-upstream:latest",
			denyMsg: "signature is required for latest tag",
		},
		{
			name:  "dockerfile_upstream_master_allowed_with_signature_any_ref",
			sig:   dockerGithubBuilderSig("moby/buildkit", "refs/heads/master"),
			ref:   "docker/dockerfile-upstream:master",
			allow: true,
		},
		{
			name:    "dockerfile_upstream_master_denied_without_signature",
			ref:     "docker/dockerfile-upstream:master",
			denyMsg: "signature is required for master tag",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := makeDefaultPolicy(t, tc.sig)
			resp := runDefaultPolicyImage(t, p, tc.ref)
			if tc.allow {
				require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_ALLOW, resp.Action)
				require.Empty(t, resp.DenyMessages)
				return
			}

			require.Equal(t, moby_buildkit_v1_sourcepolicy.PolicyAction_DENY, resp.Action)
			require.Len(t, resp.DenyMessages, 1)
			require.Contains(t, resp.DenyMessages[0].Message, tc.denyMsg)
		})
	}
}
