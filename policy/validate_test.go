package policy

import (
	"context"
	"crypto/sha1" //nolint:gosec // used for git object checksums in tests
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	policyimage "github.com/moby/policy-helpers/image"
	policytypes "github.com/moby/policy-helpers/types"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/stretchr/testify/require"
)

func TestSourceToInputWithLogger(t *testing.T) {
	tm := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name      string
		src       *gwpb.ResolveSourceMetaResponse
		platform  *ocispecs.Platform
		verifier  PolicyVerifierProvider
		expInput  Input
		expUnk    []string
		expErrMsg string
		assert    func(*testing.T, Input, []string, error)
	}{
		{
			name:      "nil-source-metadata",
			src:       nil,
			expErrMsg: "no source info in request",
		},
		{
			name: "invalid-source-identifier",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{Identifier: "not-a-source"},
			},
			expErrMsg: "invalid source identifier: not-a-source",
		},
		{
			name: "http-source-with-checksum-and-auth",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "https://example.com/foo.tar.gz?download=1",
					Attrs: map[string]string{
						pb.AttrHTTPAuthHeaderSecret: "my-secret",
					},
				},
				HTTP: &gwpb.ResolveSourceHTTPResponse{
					Checksum: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				},
			},
			expInput: Input{
				HTTP: &HTTP{
					URL:      "https://example.com/foo.tar.gz?download=1",
					Schema:   "https",
					Host:     "example.com",
					Path:     "/foo.tar.gz",
					Query:    map[string][]string{"download": {"1"}},
					HasAuth:  true,
					Checksum: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				},
			},
		},
		{
			name: "http-source-without-checksum",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "http://example.com/archive.tgz",
				},
			},
			expInput: Input{
				HTTP: &HTTP{
					URL:    "http://example.com/archive.tgz",
					Schema: "http",
					Host:   "example.com",
					Path:   "/archive.tgz",
					Query:  map[string][]string{},
				},
			},
			expUnk: []string{"input.http.checksum"},
		},
		{
			name: "http-with-query-and-fragment-parses-fields-correctly",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "https://example.com/a/b.tar.gz?x=1&x=2#frag",
				},
				HTTP: &gwpb.ResolveSourceHTTPResponse{
					Checksum: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				},
			},
			expInput: Input{
				HTTP: &HTTP{
					URL:      "https://example.com/a/b.tar.gz?x=1&x=2#frag",
					Schema:   "https",
					Host:     "example.com",
					Path:     "/a/b.tar.gz",
					Query:    map[string][]string{"x": {"1", "2"}},
					Checksum: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				},
			},
		},
		{
			name: "http-with-nil-attrs-does-not-set-auth",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "https://example.com/secure.tgz",
				},
				HTTP: &gwpb.ResolveSourceHTTPResponse{
					Checksum: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				},
			},
			expInput: Input{
				HTTP: &HTTP{
					URL:      "https://example.com/secure.tgz",
					Schema:   "https",
					Host:     "example.com",
					Path:     "/secure.tgz",
					Query:    map[string][]string{},
					Checksum: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				},
			},
		},
		{
			name: "local-source",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "local://context",
				},
			},
			expInput: Input{
				Local: &Local{Name: "context"},
			},
		},
		{
			name: "image-source-without-platform",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
			},
			expErrMsg: "platform required for image source",
		},
		{
			name: "image-source-without-resolved-metadata",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			expInput: Input{
				Image: &Image{
					Ref:          "docker.io/library/alpine:latest",
					Host:         "docker.io",
					Repo:         "alpine",
					FullRepo:     "docker.io/library/alpine",
					Tag:          "latest",
					Platform:     "linux/amd64",
					OS:           "linux",
					Architecture: "amd64",
				},
			},
			expUnk: []string{
				"input.image.checksum",
				"input.image.labels",
				"input.image.user",
				"input.image.volumes",
				"input.image.workingDir",
				"input.image.env",
				"input.image.hasProvenance",
				"input.image.provenance",
				"input.image.signatures",
			},
		},
		{
			name: "docker-image-canonical-ref-does-not-request-checksum-unknown",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			expInput: Input{
				Image: &Image{
					Ref:          "docker.io/library/alpine@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					Host:         "docker.io",
					Repo:         "alpine",
					FullRepo:     "docker.io/library/alpine",
					IsCanonical:  true,
					Checksum:     "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					Platform:     "linux/amd64",
					OS:           "linux",
					Architecture: "amd64",
				},
			},
			expUnk: []string{
				"input.image.labels",
				"input.image.user",
				"input.image.volumes",
				"input.image.workingDir",
				"input.image.env",
				"input.image.hasProvenance",
				"input.image.provenance",
				"input.image.signatures",
			},
		},
		{
			name: "docker-image-invalid-config-bytes-returns-error",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
					Config: []byte("{"),
				},
			},
			platform:  &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			expErrMsg: "failed to unmarshal image config",
		},
		{
			name: "image-attestation-chain-sets-has-provenance-without-verifier",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest: "sha256:abababababababababababababababababababababababababababababababab",
					AttestationChain: &gwpb.AttestationChain{
						AttestationManifest: "sha256:bcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbcbc",
					},
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			expInput: Input{
				Image: &Image{
					Ref:           "docker.io/library/alpine:latest",
					Host:          "docker.io",
					Repo:          "alpine",
					FullRepo:      "docker.io/library/alpine",
					Tag:           "latest",
					Platform:      "linux/amd64",
					OS:            "linux",
					Architecture:  "amd64",
					Checksum:      "sha256:abababababababababababababababababababababababababababababababab",
					HasProvenance: true,
				},
			},
			expUnk: []string{
				"input.image.labels",
				"input.image.user",
				"input.image.volumes",
				"input.image.workingDir",
				"input.image.env",
			},
		},
		{
			name: "image-attestation-chain-with-mock-verifier-sets-signature-properties",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest:           "sha256:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
					AttestationChain: newTestAttestationChain(t),
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			verifier: func() (PolicyVerifier, error) {
				return &mockPolicyVerifier{
					verifyImage: func(context.Context, policyimage.ReferrersProvider, ocispecs.Descriptor, *ocispecs.Platform) (*policytypes.SignatureInfo, error) {
						ts := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
						return &policytypes.SignatureInfo{
							Kind:            policytypes.KindDockerGithubBuilder,
							SignatureType:   policytypes.SignatureSimpleSigningV1,
							DockerReference: "docker.io/library/alpine:latest",
							IsDHI:           true,
							Timestamps: []policytypes.TimestampVerificationResult{
								{Type: "rekor", URI: "https://rekor.sigstore.dev", Timestamp: ts},
							},
							Signer: &certificate.Summary{
								CertificateIssuer:      "https://token.actions.githubusercontent.com",
								SubjectAlternativeName: "https://github.com/docker/buildx/.github/workflows/ci.yml@refs/heads/main",
								Extensions: certificate.Extensions{
									BuildSignerURI:             "https://github.com/docker/buildx/.github/workflows/ci.yml",
									BuildSignerDigest:          "sha256:1234",
									RunnerEnvironment:          "github-hosted",
									SourceRepositoryURI:        "https://github.com/docker/buildx",
									SourceRepositoryDigest:     "abcdef",
									SourceRepositoryRef:        "refs/heads/main",
									SourceRepositoryOwnerURI:   "https://github.com/docker",
									BuildConfigURI:             "https://github.com/docker/buildx/.github/workflows/ci.yml",
									BuildConfigDigest:          "sha256:5678",
									RunInvocationURI:           "https://github.com/docker/buildx/actions/runs/1",
									SourceRepositoryIdentifier: "docker/buildx",
								},
							},
						}, nil
					},
				}, nil
			},
			assert: func(t *testing.T, inp Input, unknowns []string, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Equal(t, []string{
					"input.image.labels",
					"input.image.user",
					"input.image.volumes",
					"input.image.workingDir",
					"input.image.env",
				}, unknowns)
				require.NotNil(t, inp.Image)
				require.True(t, inp.Image.HasProvenance)
				require.Len(t, inp.Image.Signatures, 1)
				sig := inp.Image.Signatures[0]
				require.Equal(t, SignatureKindDockerGithubBuilder, sig.SignatureKind)
				require.Equal(t, SignatureTypeSimpleSigningV1, sig.SignatureType)
				require.Equal(t, "docker.io/library/alpine:latest", sig.DockerReference)
				require.True(t, sig.IsDHI)
				require.Len(t, sig.Timestamps, 1)
				require.Equal(t, "rekor", sig.Timestamps[0].Type)
				require.Equal(t, "https://rekor.sigstore.dev", sig.Timestamps[0].URI)
				require.NotNil(t, sig.Signer)
				require.Equal(t, "https://token.actions.githubusercontent.com", sig.Signer.CertificateIssuer)
				require.Equal(t, "https://github.com/docker/buildx/.github/workflows/ci.yml@refs/heads/main", sig.Signer.SubjectAlternativeName)
				require.Equal(t, "https://github.com/docker/buildx/.github/workflows/ci.yml", sig.Signer.BuildSignerURI)
				require.Equal(t, "sha256:1234", sig.Signer.BuildSignerDigest)
				require.Equal(t, "github-hosted", sig.Signer.RunnerEnvironment)
				require.Equal(t, "https://github.com/docker/buildx", sig.Signer.SourceRepositoryURI)
				require.Equal(t, "abcdef", sig.Signer.SourceRepositoryDigest)
				require.Equal(t, "refs/heads/main", sig.Signer.SourceRepositoryRef)
				require.Equal(t, "https://github.com/docker", sig.Signer.SourceRepositoryOwnerURI)
				require.Equal(t, "https://github.com/docker/buildx/.github/workflows/ci.yml", sig.Signer.BuildConfigURI)
				require.Equal(t, "sha256:5678", sig.Signer.BuildConfigDigest)
				require.Equal(t, "https://github.com/docker/buildx/actions/runs/1", sig.Signer.RunInvocationURI)
				require.Equal(t, "docker/buildx", sig.Signer.SourceRepositoryIdentifier)
			},
		},
		{
			name: "image-attestation-chain-loads-provenance-fields-v0.2",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest:           "sha256:efefefefefefefefefefefefefefefefefefefefefefefefefefefefefefefef",
					AttestationChain: newTestAttestationChainWithProvenance(t),
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			assert: func(t *testing.T, inp Input, unknowns []string, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Equal(t, []string{
					"input.image.labels",
					"input.image.user",
					"input.image.volumes",
					"input.image.workingDir",
					"input.image.env",
				}, unknowns)
				require.NotNil(t, inp.Image)
				require.True(t, inp.Image.HasProvenance)
				require.NotNil(t, inp.Image.Provenance)
				require.Equal(t, slsa02.PredicateSLSAProvenance, inp.Image.Provenance.PredicateType)
				require.Equal(t, "https://example.com/build-type", inp.Image.Provenance.BuildType)
				require.Equal(t, "https://example.com/builder-id", inp.Image.Provenance.BuilderID)
				require.Equal(t, "inv-v02", inp.Image.Provenance.InvocationID)
				require.Equal(t, "2024-01-02T03:04:05Z", inp.Image.Provenance.StartedOn)
				require.Equal(t, "2024-01-02T03:05:05Z", inp.Image.Provenance.FinishedOn)
				require.Equal(t, "gateway.v0", inp.Image.Provenance.Frontend)
				require.Equal(t, map[string]string{"BUILDKIT_CONTEXT_KEEP_GIT_DIR": "1"}, inp.Image.Provenance.BuildArgs)
				require.Equal(t, map[string]string{
					"build-arg:BUILDKIT_CONTEXT_KEEP_GIT_DIR": "1",
					"cmdline": "docker/dockerfile-upstream:master",
				}, inp.Image.Provenance.RawArgs)
				require.NotNil(t, inp.Image.Provenance.ConfigSource)
				require.Equal(t, "https://github.com/moby/buildkit.git#refs/tags/v0.21.0", inp.Image.Provenance.ConfigSource.URI)
				require.Equal(t, "Dockerfile", inp.Image.Provenance.ConfigSource.Path)
				require.Equal(t, map[string]string{"sha1": "52b004d2afe20c5c80967cc1784e718b52d69dae"}, inp.Image.Provenance.ConfigSource.Digest)
				require.NotNil(t, inp.Image.Provenance.Completeness)
				require.NotNil(t, inp.Image.Provenance.Completeness.Parameters)
				require.True(t, *inp.Image.Provenance.Completeness.Parameters)
				require.NotNil(t, inp.Image.Provenance.Completeness.Environment)
				require.True(t, *inp.Image.Provenance.Completeness.Environment)
				require.NotNil(t, inp.Image.Provenance.Completeness.Materials)
				require.False(t, *inp.Image.Provenance.Completeness.Materials)
				require.NotNil(t, inp.Image.Provenance.Reproducible)
				require.True(t, *inp.Image.Provenance.Reproducible)
				require.NotNil(t, inp.Image.Provenance.Hermetic)
				require.True(t, *inp.Image.Provenance.Hermetic)
			},
		},
		{
			name: "image-attestation-chain-loads-provenance-fields-v1",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest:           "sha256:f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0",
					AttestationChain: newTestAttestationChainWithProvenanceV1(t),
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			assert: func(t *testing.T, inp Input, unknowns []string, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Equal(t, []string{
					"input.image.labels",
					"input.image.user",
					"input.image.volumes",
					"input.image.workingDir",
					"input.image.env",
				}, unknowns)
				require.NotNil(t, inp.Image)
				require.True(t, inp.Image.HasProvenance)
				require.NotNil(t, inp.Image.Provenance)
				require.Equal(t, slsa1.PredicateSLSAProvenance, inp.Image.Provenance.PredicateType)
				require.Equal(t, "https://example.com/build-type-v1", inp.Image.Provenance.BuildType)
				require.Equal(t, "https://example.com/builder-id-v1", inp.Image.Provenance.BuilderID)
				require.Equal(t, "inv-v1", inp.Image.Provenance.InvocationID)
				require.Equal(t, "2024-02-03T04:05:06Z", inp.Image.Provenance.StartedOn)
				require.Equal(t, "2024-02-03T04:06:06Z", inp.Image.Provenance.FinishedOn)
				require.Equal(t, "gateway.v0", inp.Image.Provenance.Frontend)
				require.Equal(t, map[string]string{"BUILDKIT_CONTEXT_KEEP_GIT_DIR": "1"}, inp.Image.Provenance.BuildArgs)
				require.Equal(t, map[string]string{
					"build-arg:BUILDKIT_CONTEXT_KEEP_GIT_DIR": "1",
					"source": "docker/dockerfile-upstream:master",
				}, inp.Image.Provenance.RawArgs)
				require.NotNil(t, inp.Image.Provenance.ConfigSource)
				require.Equal(t, "https://github.com/moby/buildkit.git#refs/heads/master", inp.Image.Provenance.ConfigSource.URI)
				require.Equal(t, "Dockerfile", inp.Image.Provenance.ConfigSource.Path)
				require.Equal(t, map[string]string{"sha1": "9836771d0c5b21cbc7f0c38b81be39c42fc46b7b"}, inp.Image.Provenance.ConfigSource.Digest)
				require.NotNil(t, inp.Image.Provenance.Completeness)
				require.NotNil(t, inp.Image.Provenance.Completeness.Parameters)
				require.True(t, *inp.Image.Provenance.Completeness.Parameters)
				require.Nil(t, inp.Image.Provenance.Completeness.Environment)
				require.NotNil(t, inp.Image.Provenance.Completeness.Materials)
				require.False(t, *inp.Image.Provenance.Completeness.Materials)
				require.NotNil(t, inp.Image.Provenance.Reproducible)
				require.True(t, *inp.Image.Provenance.Reproducible)
				require.NotNil(t, inp.Image.Provenance.Hermetic)
				require.True(t, *inp.Image.Provenance.Hermetic)
			},
		},
		{
			name: "image-attestation-chain-without-manifest-keeps-has-provenance-false",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest: "sha256:babababababababababababababababababababababababababababababababa",
					AttestationChain: &gwpb.AttestationChain{
						AttestationManifest: "",
					},
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			expInput: Input{
				Image: &Image{
					Ref:          "docker.io/library/alpine:latest",
					Host:         "docker.io",
					Repo:         "alpine",
					FullRepo:     "docker.io/library/alpine",
					Tag:          "latest",
					Platform:     "linux/amd64",
					OS:           "linux",
					Architecture: "amd64",
					Checksum:     "sha256:babababababababababababababababababababababababababababababababa",
				},
			},
			expUnk: []string{
				"input.image.labels",
				"input.image.user",
				"input.image.volumes",
				"input.image.workingDir",
				"input.image.env",
			},
			assert: func(t *testing.T, inp Input, unknowns []string, err error) {
				t.Helper()
				require.NoError(t, err)
				require.NotNil(t, inp.Image)
				require.False(t, inp.Image.HasProvenance)
				require.NotContains(t, unknowns, "input.image.hasProvenance")
				require.NotContains(t, unknowns, "input.image.signatures")
			},
		},
		{
			name: "image-source-with-config-and-no-attestation-chain",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "docker-image://alpine:latest",
				},
				Image: &gwpb.ResolveSourceImageResponse{
					Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					Config: mustMarshalImageConfig(t, ocispecs.Image{
						Created: &tm,
						Config: ocispecs.ImageConfig{
							Labels: map[string]string{"a": "b"},
							Env:    []string{"A=B"},
							User:   "root",
							Volumes: map[string]struct{}{
								"/data": {},
							},
							WorkingDir: "/work",
						},
					}),
				},
			},
			platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			expInput: Input{
				Image: &Image{
					Ref:          "docker.io/library/alpine:latest",
					Host:         "docker.io",
					Repo:         "alpine",
					FullRepo:     "docker.io/library/alpine",
					Tag:          "latest",
					Platform:     "linux/amd64",
					OS:           "linux",
					Architecture: "amd64",
					Checksum:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					CreatedTime:  "2024-01-02T03:04:05Z",
					Labels:       map[string]string{"a": "b"},
					Env:          []string{"A=B"},
					User:         "root",
					Volumes:      []string{"/data"},
					WorkingDir:   "/work",
				},
			},
			expUnk: []string{"input.image.hasProvenance", "input.image.provenance", "input.image.signatures"},
		},
		{
			name: "git-source-missing-full-remote-url-attr",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema: "https",
					Host:   "github.com",
					Remote: "https://github.com/docker/buildx.git",
				},
			},
			expUnk: []string{
				"input.git.tagName",
				"input.git.branch",
				"input.git.ref",
				"input.git.checksum",
				"input.git.isAnnotatedTag",
				"input.git.commitChecksum",
				"input.git.isSHA256",
				"input.git.tag",
				"input.git.commit",
			},
		},
		{
			name: "git-source-with-full-remote-url-attr-uses-attr",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
					Attrs: map[string]string{
						pb.AttrFullRemoteURL: "https://github.com/docker/buildx.git",
					},
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:  "https",
					Host:    "github.com",
					Remote:  "https://github.com/docker/buildx.git",
					FullURL: "https://github.com/docker/buildx.git",
				},
			},
			expUnk: []string{
				"input.git.tagName",
				"input.git.branch",
				"input.git.ref",
				"input.git.checksum",
				"input.git.isAnnotatedTag",
				"input.git.commitChecksum",
				"input.git.isSHA256",
				"input.git.tag",
				"input.git.commit",
			},
		},
		{
			name: "git-source-with-full-remote-url-attr-ssh-uses-attr",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
					Attrs: map[string]string{
						pb.AttrFullRemoteURL: "ssh://git@github.com/docker/buildx.git",
					},
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:  "ssh",
					Host:    "github.com",
					Remote:  "ssh://git@github.com/docker/buildx.git",
					FullURL: "ssh://git@github.com/docker/buildx.git",
				},
			},
			expUnk: []string{
				"input.git.tagName",
				"input.git.branch",
				"input.git.ref",
				"input.git.checksum",
				"input.git.isAnnotatedTag",
				"input.git.commitChecksum",
				"input.git.isSHA256",
				"input.git.tag",
				"input.git.commit",
			},
		},
		{
			name: "git-source-with-full-remote-url-attr-ssh2-uses-attr",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
					Attrs: map[string]string{
						pb.AttrFullRemoteURL: "git@github.com:docker/buildx.git",
					},
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:  "ssh",
					Host:    "github.com",
					Remote:  "git@github.com:docker/buildx.git",
					FullURL: "git@github.com:docker/buildx.git",
				},
			},
			expUnk: []string{
				"input.git.tagName",
				"input.git.branch",
				"input.git.ref",
				"input.git.checksum",
				"input.git.isAnnotatedTag",
				"input.git.commitChecksum",
				"input.git.isSHA256",
				"input.git.tag",
				"input.git.commit",
			},
		},
		{
			name: "git-source-with-full-remote-url-attr-ssh-meta-with-objects-sets-commit-and-tag",
			src: func() *gwpb.ResolveSourceMetaResponse {
				commitRaw := []byte("" +
					"tree 0123456789abcdef0123456789abcdef01234567\n" +
					"author Alice <alice@example.com> 1700000000 +0000\n" +
					"committer Bob <bob@example.com> 1700003600 +0000\n" +
					"\n" +
					"hello from commit\n")
				commitSHA := gitObjectSHA1("commit", commitRaw)
				tagRaw := []byte("" +
					"object " + commitSHA + "\n" +
					"type commit\n" +
					"tag v1.2.3\n" +
					"tagger Carol <carol@example.com> 1700007200 +0000\n" +
					"\n" +
					"release v1.2.3\n")
				tagSHA := gitObjectSHA1("tag", tagRaw)
				return &gwpb.ResolveSourceMetaResponse{
					Source: &pb.SourceOp{
						Identifier: "git://github.com/docker/buildx.git",
						Attrs: map[string]string{
							pb.AttrFullRemoteURL: "ssh://git@github.com/docker/buildx.git",
						},
					},
					Git: &gwpb.ResolveSourceGitResponse{
						Ref:            "refs/tags/v1.2.3",
						Checksum:       tagSHA,
						CommitChecksum: commitSHA,
						CommitObject:   commitRaw,
						TagObject:      tagRaw,
					},
				}
			}(),
			assert: func(t *testing.T, inp Input, unknowns []string, err error) {
				t.Helper()
				require.NoError(t, err)
				require.Empty(t, unknowns)
				require.NotNil(t, inp.Git)
				require.Equal(t, "ssh", inp.Git.Schema)
				require.Equal(t, "github.com", inp.Git.Host)
				require.Equal(t, "ssh://git@github.com/docker/buildx.git", inp.Git.Remote)
				require.Equal(t, "ssh://git@github.com/docker/buildx.git", inp.Git.FullURL)
				require.Equal(t, "refs/tags/v1.2.3", inp.Git.Ref)
				require.Equal(t, "v1.2.3", inp.Git.TagName)
				require.True(t, inp.Git.IsAnnotatedTag)
				require.NotEmpty(t, inp.Git.Checksum)
				require.NotEmpty(t, inp.Git.CommitChecksum)
				require.NotNil(t, inp.Git.Commit)
				require.Equal(t, "0123456789abcdef0123456789abcdef01234567", inp.Git.Commit.Tree)
				require.Equal(t, "hello from commit", inp.Git.Commit.Message)
				require.Equal(t, "Alice", inp.Git.Commit.Author.Name)
				require.Equal(t, "alice@example.com", inp.Git.Commit.Author.Email)
				require.Equal(t, "Bob", inp.Git.Commit.Committer.Name)
				require.Equal(t, "bob@example.com", inp.Git.Commit.Committer.Email)
				require.NotNil(t, inp.Git.Tag)
				require.Equal(t, inp.Git.CommitChecksum, inp.Git.Tag.Object)
				require.Equal(t, "commit", inp.Git.Tag.Type)
				require.Equal(t, "v1.2.3", inp.Git.Tag.Tag)
				require.Equal(t, "release v1.2.3", inp.Git.Tag.Message)
				require.Equal(t, "Carol", inp.Git.Tag.Tagger.Name)
				require.Equal(t, "carol@example.com", inp.Git.Tag.Tagger.Email)
			},
		},
		{
			name: "git-meta-ref-heads-main-sets-branch",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
				Git: &gwpb.ResolveSourceGitResponse{
					Ref:      "refs/heads/main",
					Checksum: "1111111111111111111111111111111111111111",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:         "https",
					Host:           "github.com",
					Remote:         "https://github.com/docker/buildx.git",
					Ref:            "refs/heads/main",
					Branch:         "main",
					Checksum:       "1111111111111111111111111111111111111111",
					CommitChecksum: "1111111111111111111111111111111111111111",
				},
			},
			expUnk: []string{"input.git.commit", "input.git.tag"},
		},
		{
			name: "git-meta-ref-tags-v1-sets-tag-name",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
				Git: &gwpb.ResolveSourceGitResponse{
					Ref:      "refs/tags/v1.2.3",
					Checksum: "2222222222222222222222222222222222222222",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:         "https",
					Host:           "github.com",
					Remote:         "https://github.com/docker/buildx.git",
					Ref:            "refs/tags/v1.2.3",
					TagName:        "v1.2.3",
					Checksum:       "2222222222222222222222222222222222222222",
					CommitChecksum: "2222222222222222222222222222222222222222",
				},
			},
			expUnk: []string{"input.git.commit", "input.git.tag"},
		},
		{
			name: "git-meta-empty-commit-checksum-falls-back-to-checksum",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
				Git: &gwpb.ResolveSourceGitResponse{
					Checksum: "3333333333333333333333333333333333333333",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:         "https",
					Host:           "github.com",
					Remote:         "https://github.com/docker/buildx.git",
					Checksum:       "3333333333333333333333333333333333333333",
					CommitChecksum: "3333333333333333333333333333333333333333",
				},
			},
			expUnk: []string{"input.git.commit", "input.git.tag"},
		},
		{
			name: "git-meta-sha256-checksum-sets-is-sha256",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
				Git: &gwpb.ResolveSourceGitResponse{
					Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:         "https",
					Host:           "github.com",
					Remote:         "https://github.com/docker/buildx.git",
					Checksum:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					CommitChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					IsSHA256:       true,
				},
			},
			expUnk: []string{"input.git.commit", "input.git.tag"},
		},
		{
			name: "git-meta-checksum-ne-commit-checksum-sets-annotated-tag",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
				Git: &gwpb.ResolveSourceGitResponse{
					Checksum:       "4444444444444444444444444444444444444444",
					CommitChecksum: "5555555555555555555555555555555555555555",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:         "https",
					Host:           "github.com",
					Remote:         "https://github.com/docker/buildx.git",
					Checksum:       "4444444444444444444444444444444444444444",
					CommitChecksum: "5555555555555555555555555555555555555555",
					IsAnnotatedTag: true,
				},
			},
			expUnk: []string{"input.git.commit", "input.git.tag"},
		},
		{
			name: "git-meta-missing-commit-object-adds-commit-and-tag-unknowns",
			src: &gwpb.ResolveSourceMetaResponse{
				Source: &pb.SourceOp{
					Identifier: "git://github.com/docker/buildx.git",
				},
				Git: &gwpb.ResolveSourceGitResponse{
					Ref:            "refs/heads/main",
					Checksum:       "6666666666666666666666666666666666666666",
					CommitChecksum: "6666666666666666666666666666666666666666",
				},
			},
			expInput: Input{
				Git: &Git{
					Schema:         "https",
					Host:           "github.com",
					Remote:         "https://github.com/docker/buildx.git",
					Ref:            "refs/heads/main",
					Branch:         "main",
					Checksum:       "6666666666666666666666666666666666666666",
					CommitChecksum: "6666666666666666666666666666666666666666",
				},
			},
			expUnk: []string{"input.git.commit", "input.git.tag"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inp, unknowns, err := SourceToInputWithLogger(t.Context(), tc.verifier, tc.src, tc.platform, nil)
			if tc.assert != nil {
				tc.assert(t, inp, unknowns, err)
				return
			}
			if tc.expErrMsg != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErrMsg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expInput, inp)
			require.Equal(t, tc.expUnk, unknowns)
		})
	}
}

func mustMarshalImageConfig(t *testing.T, img ocispecs.Image) []byte {
	t.Helper()
	dt, err := json.Marshal(img)
	require.NoError(t, err)
	return dt
}

func gitObjectSHA1(objType string, raw []byte) string {
	prefix := fmt.Appendf(nil, "%s %d\x00", objType, len(raw))
	//nolint:gosec // Git object IDs are defined using SHA-1 for this test fixture.
	sum := sha1.Sum(append(prefix, raw...))
	return hex.EncodeToString(sum[:])
}

type mockPolicyVerifier struct {
	verifyImage func(context.Context, policyimage.ReferrersProvider, ocispecs.Descriptor, *ocispecs.Platform) (*policytypes.SignatureInfo, error)
}

func (m *mockPolicyVerifier) VerifyImage(ctx context.Context, provider policyimage.ReferrersProvider, desc ocispecs.Descriptor, platform *ocispecs.Platform) (*policytypes.SignatureInfo, error) {
	return m.verifyImage(ctx, provider, desc, platform)
}

func newTestAttestationChain(t *testing.T) *gwpb.AttestationChain {
	t.Helper()

	imgDigest := digest.FromString("image-manifest")
	attDigest := digest.FromString("attestation-manifest")

	indexBytes := mustMarshalJSON(t, map[string]any{
		"mediaType": ocispecs.MediaTypeImageIndex,
		"manifests": []map[string]any{
			{
				"mediaType": ocispecs.MediaTypeImageManifest,
				"digest":    imgDigest.String(),
				"size":      int64(10),
				"platform": map[string]any{
					"os":           "linux",
					"architecture": "amd64",
				},
			},
			{
				"mediaType": ocispecs.MediaTypeImageManifest,
				"digest":    attDigest.String(),
				"size":      int64(10),
				"annotations": map[string]string{
					policyimage.AnnotationDockerReferenceType:   policyimage.AttestationManifestType,
					policyimage.AnnotationDockerReferenceDigest: imgDigest.String(),
				},
			},
		},
	})
	indexDigest := digest.FromBytes(indexBytes)

	sigManifestBytes := mustMarshalJSON(t, map[string]any{
		"schemaVersion": 2,
		"mediaType":     ocispecs.MediaTypeImageManifest,
		"artifactType":  policyimage.ArtifactTypeSigstoreBundle,
	})
	sigDigest := digest.FromBytes(sigManifestBytes)

	return &gwpb.AttestationChain{
		Root:                indexDigest.String(),
		AttestationManifest: attDigest.String(),
		SignatureManifests:  []string{sigDigest.String()},
		Blobs: map[string]*gwpb.Blob{
			indexDigest.String(): {
				Descriptor_: &gwpb.Descriptor{
					MediaType: ocispecs.MediaTypeImageIndex,
					Digest:    indexDigest.String(),
					Size:      int64(len(indexBytes)),
				},
				Data: indexBytes,
			},
			sigDigest.String(): {
				Descriptor_: &gwpb.Descriptor{
					MediaType: ocispecs.MediaTypeImageManifest,
					Digest:    sigDigest.String(),
					Size:      int64(len(sigManifestBytes)),
				},
				Data: sigManifestBytes,
			},
		},
	}
}

func newTestAttestationChainWithProvenance(t *testing.T) *gwpb.AttestationChain {
	t.Helper()

	ac := newTestAttestationChain(t)
	provenancePredicate := map[string]any{
		"builder": map[string]any{
			"id": "https://example.com/builder-id",
		},
		"buildType": "https://example.com/build-type",
		"invocation": map[string]any{
			"configSource": map[string]any{
				"digest": map[string]any{
					"sha1": "52b004d2afe20c5c80967cc1784e718b52d69dae",
				},
				"entryPoint": "Dockerfile",
				"uri":        "https://github.com/moby/buildkit.git#refs/tags/v0.21.0",
			},
			"parameters": map[string]any{
				"frontend": "gateway.v0",
				"args": map[string]any{
					"build-arg:BUILDKIT_CONTEXT_KEEP_GIT_DIR": "1",
					"cmdline": "docker/dockerfile-upstream:master",
				},
			},
			"environment": map[string]any{
				"platform": "linux/amd64",
			},
		},
		"metadata": map[string]any{
			"buildInvocationID": "inv-v02",
			"buildStartedOn":    "2024-01-02T03:04:05Z",
			"buildFinishedOn":   "2024-01-02T03:05:05Z",
			"completeness": map[string]any{
				"parameters":  true,
				"environment": true,
				"materials":   false,
			},
			"reproducible": true,
			"https://mobyproject.org/buildkit@v1#hermetic": true,
		},
	}
	provenanceBytes := mustMarshalJSON(t, map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": slsa02.PredicateSLSAProvenance,
		"predicate":     provenancePredicate,
	})
	provenanceDigest := digest.FromBytes(provenanceBytes)

	ac.Blobs[provenanceDigest.String()] = &gwpb.Blob{
		Descriptor_: &gwpb.Descriptor{
			MediaType: "application/vnd.in-toto+json",
			Digest:    provenanceDigest.String(),
			Size:      int64(len(provenanceBytes)),
			Annotations: map[string]string{
				predicateTypeAnnotation: slsa02.PredicateSLSAProvenance,
			},
		},
		Data: provenanceBytes,
	}

	return ac
}

func newTestAttestationChainWithProvenanceV1(t *testing.T) *gwpb.AttestationChain {
	t.Helper()

	ac := newTestAttestationChain(t)
	provenancePredicate := map[string]any{
		"buildDefinition": map[string]any{
			"buildType": "https://example.com/build-type-v1",
			"externalParameters": map[string]any{
				"configSource": map[string]any{
					"digest": map[string]any{
						"sha1": "9836771d0c5b21cbc7f0c38b81be39c42fc46b7b",
					},
					"path": "Dockerfile",
					"uri":  "https://github.com/moby/buildkit.git#refs/heads/master",
				},
				"request": map[string]any{
					"frontend": "gateway.v0",
					"args": map[string]any{
						"build-arg:BUILDKIT_CONTEXT_KEEP_GIT_DIR": "1",
						"source": "docker/dockerfile-upstream:master",
					},
				},
			},
			"internalParameters": map[string]any{},
		},
		"runDetails": map[string]any{
			"builder": map[string]any{
				"id": "https://example.com/builder-id-v1",
			},
			"metadata": map[string]any{
				"invocationID": "inv-v1",
				"startedOn":    "2024-02-03T04:05:06Z",
				"finishedOn":   "2024-02-03T04:06:06Z",
				"buildkit_completeness": map[string]any{
					"request":              true,
					"resolvedDependencies": false,
				},
				"buildkit_reproducible": true,
				"buildkit_hermetic":     true,
			},
		},
	}
	provenanceBytes := mustMarshalJSON(t, map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": slsa1.PredicateSLSAProvenance,
		"predicate":     provenancePredicate,
	})
	provenanceDigest := digest.FromBytes(provenanceBytes)

	ac.Blobs[provenanceDigest.String()] = &gwpb.Blob{
		Descriptor_: &gwpb.Descriptor{
			MediaType: "application/vnd.in-toto+json",
			Digest:    provenanceDigest.String(),
			Size:      int64(len(provenanceBytes)),
			Annotations: map[string]string{
				predicateTypeAnnotation: slsa1.PredicateSLSAProvenance,
			},
		},
		Data: provenanceBytes,
	}

	return ac
}

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	dt, err := json.Marshal(v)
	require.NoError(t, err)
	return dt
}
