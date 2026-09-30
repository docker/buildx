package replay

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/replay"
	"github.com/docker/buildx/util/buildflags"
	"github.com/moby/buildkit/client"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	solverpb "github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestFilterSubjectsByPlatform(t *testing.T) {
	amd := &replay.Subject{Descriptor: ocispecs.Descriptor{Platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"}}}
	arm := &replay.Subject{Descriptor: ocispecs.Descriptor{Platform: &ocispecs.Platform{OS: "linux", Architecture: "arm64"}}}
	subjects := []*replay.Subject{amd, arm}
	builderDefault := func(p string) func() (ocispecs.Platform, error) {
		return func() (ocispecs.Platform, error) {
			return platforms.MustParse(p), nil
		}
	}
	noDefault := func() (ocispecs.Platform, error) {
		t.Fatal("builder default platform must not be looked up")
		return ocispecs.Platform{}, nil
	}

	// "all" keeps every subject.
	out, err := filterSubjectsByPlatform(subjects, []string{"all"}, noDefault)
	require.NoError(t, err)
	require.Len(t, out, 2)

	// An empty filter collapses to the builder's default platform, never
	// the client's.
	for _, arch := range []string{"amd64", "arm64"} {
		out, err = filterSubjectsByPlatform(subjects, nil, builderDefault("linux/"+arch))
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, arch, out[0].Descriptor.Platform.Architecture)
	}

	// A builder default matching no subject is an error that lists the
	// available platforms.
	_, err = filterSubjectsByPlatform(subjects, nil, builderDefault("windows/amd64"))
	require.ErrorContains(t, err, "linux/amd64 linux/arm64")

	// A failing builder lookup is reported with the available platforms.
	_, err = filterSubjectsByPlatform(subjects, nil, func() (ocispecs.Platform, error) {
		return ocispecs.Platform{}, errors.New("builder unavailable")
	})
	require.ErrorContains(t, err, "builder unavailable")
	require.ErrorContains(t, err, "linux/amd64 linux/arm64")

	// A single platform subject is the default regardless of the builder.
	out, err = filterSubjectsByPlatform([]*replay.Subject{arm}, nil, noDefault)
	require.NoError(t, err)
	require.Equal(t, []*replay.Subject{arm}, out)

	// Explicit match on a single platform.
	out, err = filterSubjectsByPlatform(subjects, []string{"linux/arm64"}, noDefault)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "arm64", out[0].Descriptor.Platform.Architecture)

	// Comma-separated values are equivalent to repeating --platform.
	out, err = filterSubjectsByPlatform(subjects, []string{"linux/amd64,linux/arm64"}, noDefault)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "amd64", out[0].Descriptor.Platform.Architecture)
	require.Equal(t, "arm64", out[1].Descriptor.Platform.Architecture)
	outRepeated, err := filterSubjectsByPlatform(subjects, []string{"linux/amd64", "linux/arm64"}, noDefault)
	require.NoError(t, err)
	require.Equal(t, outRepeated, out)

	// Duplicate values collapse to one target, regardless of flag spelling.
	out, err = filterSubjectsByPlatform(subjects, []string{"linux/amd64,linux/amd64"}, noDefault)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "amd64", out[0].Descriptor.Platform.Architecture)

	_, err = filterSubjectsByPlatform(subjects, []string{"all,linux/amd64"}, noDefault)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be combined")

	// Explicit platform with no matching subject is an error.
	_, err = filterSubjectsByPlatform([]*replay.Subject{amd}, []string{"linux/arm64"}, noDefault)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not present")

	// Artifact selection is strict: execution compatibility must not select
	// a different architecture or variant.
	armv7 := &replay.Subject{Descriptor: ocispecs.Descriptor{Platform: &ocispecs.Platform{OS: "linux", Architecture: "arm", Variant: "v7"}}}
	_, err = filterSubjectsByPlatform([]*replay.Subject{armv7}, []string{"linux/arm64"}, noDefault)
	require.Error(t, err)

	// A single manifest or attestation without platform metadata inherits an
	// explicit requested platform so the solve itself is constrained.
	platformless := &replay.Subject{}
	out, err = filterSubjectsByPlatform([]*replay.Subject{platformless}, []string{"linux/arm64"}, noDefault)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "linux/arm64", platforms.Format(*out[0].Descriptor.Platform))
	require.Nil(t, platformless.Descriptor.Platform, "filtering must not mutate the loaded subject")
}

func TestApplyPredicateTargetPlatformFallback(t *testing.T) {
	subject := &replay.Subject{}
	pred := &replay.Predicate{}
	pred.BuildDefinition.InternalParameters.BuildConfig = &provenancetypes.BuildConfig{
		Definition: []provenancetypes.BuildStep{{
			Op: &solverpb.Op{Op: &solverpb.Op_Exec{Exec: &solverpb.ExecOp{Meta: &solverpb.Meta{
				Env: []string{"BUILDPLATFORM=linux/amd64", "TARGETPLATFORM=linux/arm64"},
			}}}},
		}},
	}

	got := applyPredicateTargetPlatformFallback(subject, pred, nil)
	require.NotSame(t, subject, got)
	require.Equal(t, "linux/arm64", platforms.Format(*got.Descriptor.Platform))
	require.Nil(t, subject.Descriptor.Platform, "defaulting must not mutate the loaded subject")

	explicit, err := filterSubjectsByPlatform([]*replay.Subject{subject}, []string{"linux/amd64"}, func() (ocispecs.Platform, error) {
		t.Fatal("builder default platform must not be looked up")
		return ocispecs.Platform{}, nil
	})
	require.NoError(t, err)
	require.Len(t, explicit, 1)
	got = applyPredicateTargetPlatformFallback(explicit[0], pred, []string{"linux/amd64"})
	require.Equal(t, "linux/amd64", platforms.Format(*got.Descriptor.Platform))
}

func TestBuildRejectsInvalidFlagsBeforeLoadingSubject(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		err  string
	}{
		{name: "format-without-dry-run", args: []string{"--format=json"}, err: "--format requires --dry-run"},
		{name: "unknown-format", args: []string{"--dry-run", "--format=yaml"}, err: `unknown --format "yaml"`},
		{name: "unknown-replay-mode", args: []string{"--replay-mode=llb"}, err: `unknown --replay-mode "llb"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builderName := ""
			cmd := buildCmd(nil, RootOptions{Builder: &builderName})
			cmd.SetArgs(append(tc.args, "docker-image://example.com/foo:latest"))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			require.EqualError(t, cmd.Execute(), tc.err)
		})
	}
}

func TestApplyExportShorthandsPush(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outputs []string
	}{
		{name: "no-output"},
		{name: "registry-output", outputs: []string{"type=registry,name=example.com/foo:bar"}},
		{name: "image-output", outputs: []string{"type=image,name=example.com/foo:bar"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			specs, err := buildflags.ParseExports(tc.outputs)
			require.NoError(t, err)
			exports, _, err := build.CreateExports(applyExportShorthands(specs, true, false))
			require.NoError(t, err)
			require.Len(t, exports, 1, "--push must not add a second pushing exporter")
			require.Equal(t, client.ExporterImage, exports[0].Type)
			require.Equal(t, "true", exports[0].Attrs["push"])
			require.Equal(t, "false", exports[0].Attrs["unpack"])
		})
	}
}

func TestPrintBuildPlan(t *testing.T) {
	plan := &replay.BuildPlan{Subjects: []replay.SubjectBuildPlan{{
		Descriptor: ocispecs.Descriptor{
			Digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Platform: &ocispecs.Platform{OS: "linux", Architecture: "arm64"},
		},
		Signature: &replay.SignatureVerification{
			Verified:               true,
			Type:                   "Sigstore Bundle",
			Identity:               "Docker GitHub Builder (docker/buildx@v0.37.1)",
			CertificateIssuer:      "CN=sigstore-intermediate,O=sigstore.dev",
			SubjectAlternativeName: "https://github.com/docker/github-builder/.github/workflows/build.yml@refs/heads/main",
			Issuer:                 "https://token.actions.githubusercontent.com",
			RunnerEnvironment:      "github-hosted",
			SourceRepositoryURI:    "https://github.com/docker/buildx",
			SourceRepositoryRef:    "refs/tags/v0.37.1",
			BuildSignerURI:         "https://github.com/docker/github-builder/.github/workflows/build.yml@refs/heads/main",
			Timestamps: []replay.SignatureTimestamp{{
				Type:      "Tlog",
				URI:       "https://rekor.sigstore.dev",
				Timestamp: time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC),
			}, {
				Type:      "TimestampAuthority",
				URI:       "https://timestamp.sigstore.dev/api/v1/timestamp",
				Timestamp: time.Date(2026, time.September, 18, 12, 0, 1, 0, time.UTC),
			}},
		},
		BuildConfig: replay.BuildPlanConfig{
			Frontend:  "gateway.v0",
			Context:   "https://github.com/docker/buildx.git#refs/tags/v0.37.1",
			Filename:  "Dockerfile",
			Target:    "binaries",
			BuildArgs: map[string]string{"ZED": "last", "ALPHA": "first"},
			Secrets:   []replay.PlanSecret{{ID: "GIT_AUTH_TOKEN", Optional: true}},
		},
		Materials: []replay.PlanMaterial{{
			URI:      "pkg:docker/golang@1.26-alpine3.23",
			Platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
			Digest:   "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Kind:     "image",
		}},
	}}}

	var out bytes.Buffer
	require.NoError(t, printBuildPlan(&out, plan))
	require.Equal(t, `Replay plan

Subject 1/1
  Platform:  linux/arm64
  Digest:    sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

Build configuration
  Frontend:    gateway.v0
  Context:     https://github.com/docker/buildx.git#refs/tags/v0.37.1
  Dockerfile:  Dockerfile
  Target:      binaries
  Build args:  ALPHA=first
               ZED=last
  Secrets:     [GIT_AUTH_TOKEN (optional)]

Sigstore Bundle
  Verified signer:       Docker GitHub Builder (docker/buildx@v0.37.1)
  Signer identity:       https://github.com/docker/github-builder/.github/workflows/build.yml@refs/heads/main
  Certificate issuer:    CN=sigstore-intermediate,O=sigstore.dev
  OIDC issuer:           https://token.actions.githubusercontent.com
  Runner environment:    github-hosted
  Source repository:     https://github.com/docker/buildx
  Source ref:            refs/tags/v0.37.1
    TYPE                 TIME                  SOURCE
    Transparency log     2026-09-18T12:00:00Z  https://rekor.sigstore.dev
    Timestamp authority  2026-09-18T12:00:01Z  https://timestamp.sigstore.dev/api/v1/timestamp

Materials (1)
  image [linux/amd64]  pkg:docker/golang@1.26-alpine3.23
                       sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`, out.String())
}
