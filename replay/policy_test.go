package replay

import (
	"context"
	"strings"
	"testing"

	buildxpolicy "github.com/docker/buildx/policy"
	slsacommon "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/common"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	solverpb "github.com/moby/buildkit/solver/pb"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	"github.com/stretchr/testify/require"
)

func TestPreferredDigestDeterministicFallback(t *testing.T) {
	set := map[string]string{
		"sha512": strings.Repeat("5", 128),
		"sha384": strings.Repeat("3", 96),
	}
	require.Equal(t, "sha384:"+strings.Repeat("3", 96), preferredDigest(set).String())

	set["sha256"] = strings.Repeat("2", 64)
	require.Equal(t, "sha256:"+strings.Repeat("2", 64), preferredDigest(set).String())
}

const (
	imageURIAlpine = "pkg:docker/alpine@3.18?platform=linux%2Famd64"
	imageSHA       = "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	imageSHAOther  = "sha256:bbbbbb0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	httpURI = "https://example.com/payload.tar"
	httpSHA = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	gitURIHTTPS = "https://github.com/moby/buildkit.git#refs/tags/v0.29.0"
	gitURIGit   = "git://github.com/moby/buildkit.git#refs/tags/v0.29.0"
	gitCommit   = "sha1:8543ce4428265d547cb009e5ad62348284497a88"
)

func predicateWithMaterials(mats ...slsa1.ResourceDescriptor) *Predicate {
	pred := &Predicate{}
	pred.BuildDefinition = provenancetypes.ProvenanceBuildDefinitionSLSA1{}
	pred.BuildDefinition.ResolvedDependencies = mats
	return pred
}

func imageMaterial(uri, sha string) slsa1.ResourceDescriptor {
	return slsa1.ResourceDescriptor{
		URI:    uri,
		Digest: slsacommon.DigestSet{"sha256": stripSHA256(sha)},
	}
}

func stripSHA256(s string) string {
	if i := len("sha256:"); len(s) > i && s[:i] == "sha256:" {
		return s[i:]
	}
	if i := len("sha1:"); len(s) > i && s[:i] == "sha1:" {
		return s[i:]
	}
	return s
}

func imageCheckRequest(uri, observed string) *policysession.CheckPolicyRequest {
	return &policysession.CheckPolicyRequest{
		Source: &gwpb.ResolveSourceMetaResponse{
			Source: &solverpb.SourceOp{Identifier: uri},
			Image:  &gwpb.ResolveSourceImageResponse{Digest: observed},
		},
	}
}

func httpCheckRequest(uri, observed string) *policysession.CheckPolicyRequest {
	return &policysession.CheckPolicyRequest{
		Source: &gwpb.ResolveSourceMetaResponse{
			Source: &solverpb.SourceOp{Identifier: uri},
			HTTP:   &gwpb.ResolveSourceHTTPResponse{Checksum: observed},
		},
	}
}

func gitCheckRequest(uri, observed string) *policysession.CheckPolicyRequest {
	return &policysession.CheckPolicyRequest{
		Source: &gwpb.ResolveSourceMetaResponse{
			Source: &solverpb.SourceOp{Identifier: uri},
			Git:    &gwpb.ResolveSourceGitResponse{CommitChecksum: observed},
		},
	}
}

func sourceCheckRequest(uri string, attrs map[string]string) *policysession.CheckPolicyRequest {
	return &policysession.CheckPolicyRequest{
		Source: &gwpb.ResolveSourceMetaResponse{
			Source: &solverpb.SourceOp{Identifier: uri, Attrs: attrs},
		},
	}
}

func TestPinIndexURIAllowAndDeny(t *testing.T) {
	pred := predicateWithMaterials(imageMaterial(imageURIAlpine, imageSHA))
	idx := NewPinIndex(pred)
	cb := ReplayPinCallback(idx)

	// Covered image URI is converted to the pinned digest from provenance.
	resp, _, err := cb(context.Background(), imageCheckRequest(imageURIAlpine, imageSHA))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, "docker-image://docker.io/library/alpine:3.18@"+imageSHA, resp.Update.Identifier)

	// Digest drift on a covered image still converts to the provenance pin.
	wrongSHA := "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	resp, _, err = cb(context.Background(), imageCheckRequest(imageURIAlpine, wrongSHA))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, "docker-image://docker.io/library/alpine:3.18@"+imageSHA, resp.Update.Identifier)
}

func TestPinIndexImagePURLCanonicalIdentifierAllowed(t *testing.T) {
	mat := imageMaterial(imageURIAlpine, imageSHA)
	src, _, err := buildxpolicy.ParseSLSAMaterial(mat)
	require.NoError(t, err)
	require.NotNil(t, src)

	idx := NewPinIndex(predicateWithMaterials(mat))
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), imageCheckRequest(src.Identifier, imageSHA))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_ALLOW, resp.Action)
}

func TestPinIndexImageCanonicalUnpinnedIdentifierAllowed(t *testing.T) {
	mat := imageMaterial("pkg:docker/docker/dockerfile-upstream@master", "sha256:02bce6c486f5bbd7b2eb6b9a16e3734110face1c70a6bacd827dcdb80c3f9a24")
	idx := NewPinIndex(predicateWithMaterials(mat))
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), imageCheckRequest("docker-image://docker.io/docker/dockerfile-upstream:master", ""))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, "docker-image://docker.io/docker/dockerfile-upstream:master@sha256:02bce6c486f5bbd7b2eb6b9a16e3734110face1c70a6bacd827dcdb80c3f9a24", resp.Update.Identifier)
}

func TestPinIndexImageCanonicalResolvedDigestMismatchConverted(t *testing.T) {
	mat := imageMaterial("pkg:docker/docker/dockerfile-upstream@master", "sha256:02bce6c486f5bbd7b2eb6b9a16e3734110face1c70a6bacd827dcdb80c3f9a24")
	idx := NewPinIndex(predicateWithMaterials(mat))
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), imageCheckRequest("docker-image://docker.io/docker/dockerfile-upstream:master", "sha256:a7308cdb4411614c503aee073f5cb4caa5245b8e89fceb41887129219da0b267"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, "docker-image://docker.io/docker/dockerfile-upstream:master@sha256:02bce6c486f5bbd7b2eb6b9a16e3734110face1c70a6bacd827dcdb80c3f9a24", resp.Update.Identifier)
}

func TestPinIndexImageCanonicalIdentifierCarriesDigestInSource(t *testing.T) {
	mat := imageMaterial("pkg:docker/docker/dockerfile-upstream@master", "sha256:02bce6c486f5bbd7b2eb6b9a16e3734110face1c70a6bacd827dcdb80c3f9a24")
	idx := NewPinIndex(predicateWithMaterials(mat))
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), imageCheckRequest("docker-image://docker.io/docker/dockerfile-upstream:master@sha256:a7308cdb4411614c503aee073f5cb4caa5245b8e89fceb41887129219da0b267", ""))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, "docker-image://docker.io/docker/dockerfile-upstream:master@sha256:02bce6c486f5bbd7b2eb6b9a16e3734110face1c70a6bacd827dcdb80c3f9a24", resp.Update.Identifier)
}

func TestPinIndexDigestQualifiedImagesDoNotCollide(t *testing.T) {
	matA := imageMaterial("pkg:docker/alpine?digest="+imageSHA+"&platform=linux%2Famd64", imageSHA)
	matB := imageMaterial("pkg:docker/alpine?digest="+imageSHAOther+"&platform=linux%2Famd64", imageSHAOther)
	cb := ReplayPinCallback(NewPinIndex(predicateWithMaterials(matA, matB)))

	for _, pinned := range []string{imageSHA, imageSHAOther} {
		resp, _, err := cb(context.Background(), imageCheckRequest("docker-image://docker.io/library/alpine@"+pinned, ""))
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, spb.PolicyAction_ALLOW, resp.Action)
	}

	resp, _, err := cb(context.Background(), imageCheckRequest("docker-image://docker.io/library/alpine", ""))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_DENY, resp.Action)
	require.Contains(t, resp.DenyMessages[0].Message, "ambiguous provenance pins")
}

func TestPinIndexUsesRequestPlatformToDisambiguateImage(t *testing.T) {
	matAMD64 := imageMaterial("pkg:docker/alpine?digest="+imageSHA+"&platform=linux%2Famd64", imageSHA)
	matARM64 := imageMaterial("pkg:docker/alpine?digest="+imageSHAOther+"&platform=linux%2Farm64", imageSHAOther)
	cb := ReplayPinCallback(NewPinIndex(predicateWithMaterials(matAMD64, matARM64)))
	req := imageCheckRequest("docker-image://docker.io/library/alpine", "")
	req.Platform = &solverpb.Platform{OS: "linux", Architecture: "amd64"}

	resp, _, err := cb(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.Equal(t, "docker-image://docker.io/library/alpine@"+imageSHA, resp.Update.Identifier)
}

func TestPinIndexUniqueDigestIgnoresRequestPlatform(t *testing.T) {
	mat := imageMaterial("pkg:docker/alpine?digest="+imageSHA+"&platform=linux%2Famd64", imageSHA)
	cb := ReplayPinCallback(NewPinIndex(predicateWithMaterials(mat)))
	req := imageCheckRequest("docker-image://docker.io/library/alpine", "")
	req.Platform = &solverpb.Platform{OS: "linux", Architecture: "arm64"}

	resp, _, err := cb(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.Equal(t, "docker-image://docker.io/library/alpine@"+imageSHA, resp.Update.Identifier)
}

func TestPinIndexAmbiguousDigestsRejectUnknownRequestPlatform(t *testing.T) {
	matAMD64 := imageMaterial("pkg:docker/alpine?digest="+imageSHA+"&platform=linux%2Famd64", imageSHA)
	matARM64 := imageMaterial("pkg:docker/alpine?digest="+imageSHAOther+"&platform=linux%2Farm64", imageSHAOther)
	cb := ReplayPinCallback(NewPinIndex(predicateWithMaterials(matAMD64, matARM64)))
	req := imageCheckRequest("docker-image://docker.io/library/alpine", "")
	req.Platform = &solverpb.Platform{OS: "linux", Architecture: "s390x"}

	resp, _, err := cb(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_DENY, resp.Action)
	require.Contains(t, resp.DenyMessages[0].Message, "no recorded pin")
	require.Contains(t, resp.DenyMessages[0].Message, "linux/s390x")
}

func TestPinIndexUnknownSourceDenied(t *testing.T) {
	idx := NewPinIndex(predicateWithMaterials(imageMaterial(imageURIAlpine, imageSHA)))
	cb := ReplayPinCallback(idx)

	// Unknown URI, unknown digest → DENY (fail-closed).
	other := "pkg:docker/ubuntu@22.04?platform=linux%2Famd64"
	resp, _, err := cb(context.Background(), imageCheckRequest(other, "sha256:deadbeef"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_DENY, resp.Action)
	require.NotEmpty(t, resp.DenyMessages)
	require.Contains(t, resp.DenyMessages[0].Message, "no provenance material matched requested source")
	require.Contains(t, resp.DenyMessages[0].Message, "\n  target: uri="+other+"@sha256:deadbeef")
	require.Contains(t, resp.DenyMessages[0].Message, "\n  provenance materials:")
	require.Contains(t, resp.DenyMessages[0].Message, "\n  - uri="+imageURIAlpine)
	require.Contains(t, resp.DenyMessages[0].Message, "canonical=docker-image://docker.io/library/alpine:3.18@"+imageSHA)
	require.Contains(t, resp.DenyMessages[0].Message, "platform=linux/amd64")
}

func TestPinIndexHTTPConvertedToRecordedChecksum(t *testing.T) {
	pred := predicateWithMaterials(slsa1.ResourceDescriptor{
		URI:    httpURI,
		Digest: slsacommon.DigestSet{"sha256": stripSHA256(httpSHA)},
	})
	idx := NewPinIndex(pred)
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), sourceCheckRequest(httpURI, nil))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, httpSHA, resp.Update.Attrs[solverpb.AttrHTTPChecksum])

	resp, _, err = cb(context.Background(), sourceCheckRequest(httpURI, resp.Update.Attrs))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_ALLOW, resp.Action)
}

func TestPinIndexIncludesRecordedConfigSource(t *testing.T) {
	pred := predicateWithMaterials()
	pred.BuildDefinition.ExternalParameters.ConfigSource.URI = httpURI
	pred.BuildDefinition.ExternalParameters.ConfigSource.Digest = slsacommon.DigestSet{
		"sha256": stripSHA256(httpSHA),
	}
	cb := ReplayPinCallback(NewPinIndex(pred))

	resp, _, err := cb(context.Background(), sourceCheckRequest(httpURI, nil))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.Equal(t, httpSHA, resp.Update.Attrs[solverpb.AttrHTTPChecksum])
}

func TestPinIndexGitSchemeNormalizationConvertedToRecordedCommit(t *testing.T) {
	pred := predicateWithMaterials(slsa1.ResourceDescriptor{
		URI:    gitURIHTTPS,
		Digest: slsacommon.DigestSet{"sha1": stripSHA256(gitCommit)},
	})
	idx := NewPinIndex(pred)
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), sourceCheckRequest(gitURIGit, nil))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, stripSHA256(gitCommit), resp.Update.Attrs[solverpb.AttrGitChecksum])

	resp, _, err = cb(context.Background(), sourceCheckRequest(gitURIGit, resp.Update.Attrs))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_ALLOW, resp.Action)
}

func TestPinIndexHTTPResolvedDigestMismatchDenied(t *testing.T) {
	pred := predicateWithMaterials(slsa1.ResourceDescriptor{
		URI:    httpURI,
		Digest: slsacommon.DigestSet{"sha256": stripSHA256(httpSHA)},
	})
	cb := ReplayPinCallback(NewPinIndex(pred))
	req := httpCheckRequest(httpURI, "sha256:2222222222222222222222222222222222222222222222222222222222222222")
	req.Source.Source.Attrs = map[string]string{solverpb.AttrHTTPChecksum: httpSHA}

	resp, _, err := cb(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_DENY, resp.Action)
	require.Contains(t, resp.DenyMessages[0].Message, "replay pin mismatch")
}

func TestPinIndexGitResolvedCommitMismatchDenied(t *testing.T) {
	pred := predicateWithMaterials(slsa1.ResourceDescriptor{
		URI:    gitURIHTTPS,
		Digest: slsacommon.DigestSet{"sha1": stripSHA256(gitCommit)},
	})
	cb := ReplayPinCallback(NewPinIndex(pred))
	req := gitCheckRequest(gitURIGit, "1111111111111111111111111111111111111111")
	req.Source.Source.Attrs = map[string]string{solverpb.AttrGitChecksum: stripSHA256(gitCommit)}

	resp, _, err := cb(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_DENY, resp.Action)
	require.Contains(t, resp.DenyMessages[0].Message, "replay pin mismatch")
}

func TestPinIndexAllowPending(t *testing.T) {
	// First callback invocation on an image source often arrives without
	// an observed digest (the source-meta roundtrip produces it). The
	// replay callback must not fail-closed on that pending shape when the
	// URI is covered.
	idx := NewPinIndex(predicateWithMaterials(imageMaterial(imageURIAlpine, imageSHA)))
	cb := ReplayPinCallback(idx)

	resp, _, err := cb(context.Background(), imageCheckRequest(imageURIAlpine, ""))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_CONVERT, resp.Action)
	require.NotNil(t, resp.Update)
	require.Equal(t, "docker-image://docker.io/library/alpine:3.18@"+imageSHA, resp.Update.Identifier)
}

func TestComposeCallbacksReplayStrict(t *testing.T) {
	// A permissive user-defined overlay must not defeat the strict replay
	// callback. Compose a pass-through first and the replay pin last.
	passthrough := policysession.PolicyCallback(func(ctx context.Context, req *policysession.CheckPolicyRequest) (*policysession.DecisionResponse, *gwpb.ResolveSourceMetaRequest, error) {
		return &policysession.DecisionResponse{Action: spb.PolicyAction_ALLOW}, nil, nil
	})
	idx := NewPinIndex(predicateWithMaterials(imageMaterial(imageURIAlpine, imageSHA)))
	combined := ComposeCallbacks(passthrough, ReplayPinCallback(idx))

	unknown := "pkg:docker/ubuntu@22.04?platform=linux%2Famd64"
	resp, _, err := combined(context.Background(), imageCheckRequest(unknown, "sha256:deadbeef"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, spb.PolicyAction_DENY, resp.Action)
}
