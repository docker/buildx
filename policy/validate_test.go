package policy

import (
	"crypto/sha1" //nolint:gosec // used for git object checksums in tests
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestSourceToInputWithLogger(t *testing.T) {
	tm := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

	tests := []struct {
		name      string
		src       *gwpb.ResolveSourceMetaResponse
		platform  *ocispecs.Platform
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
			expUnk: []string{"input.image.hasProvenance", "input.image.signatures"},
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
			inp, unknowns, err := SourceToInputWithLogger(t.Context(), nil, tc.src, tc.platform, nil)
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
