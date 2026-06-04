package replay

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/buildx/util/buildflags"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func testSubject(t *testing.T) *Subject {
	t.Helper()
	return &Subject{
		Descriptor: ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageManifest,
			Digest:    "sha256:aaaa",
			Platform:  &ocispecs.Platform{OS: "linux", Architecture: "amd64"},
		},
		attestManifest: ocispecs.Descriptor{
			MediaType: ocispecs.MediaTypeImageManifest,
			Digest:    "sha256:bbbb",
		},
	}
}

func testPredicate(secretSpecs []struct {
	id       string
	optional bool
}, locals []string) *Predicate {
	p := &Predicate{}
	for _, spec := range secretSpecs {
		p.BuildDefinition.ExternalParameters.Request.Secrets = append(
			p.BuildDefinition.ExternalParameters.Request.Secrets,
			&provenancetypes.Secret{ID: spec.id, Optional: spec.optional},
		)
	}
	for _, name := range locals {
		p.BuildDefinition.ExternalParameters.Request.Locals = append(
			p.BuildDefinition.ExternalParameters.Request.Locals,
			&provenancetypes.LocalSource{Name: name},
		)
	}
	p.BuildDefinition.ExternalParameters.Request.Frontend = "dockerfile.v0"
	p.BuildDefinition.ExternalParameters.Request.Args = map[string]string{
		"context":                "https://github.com/example/repo.git",
		"source":                 "docker/dockerfile:1.8",
		"cmdline":                "docker/dockerfile:1.8",
		"target":                 "default",
		"build-arg:EXAMPLE":      "1",
		"label:org.example.test": "yes",
	}
	p.BuildDefinition.ExternalParameters.ConfigSource.URI = "https://github.com/example/repo.git"
	p.BuildDefinition.ExternalParameters.ConfigSource.Path = "Dockerfile"
	p.BuildDefinition.ResolvedDependencies = []slsa1.ResourceDescriptor{
		{URI: "pkg:docker/alpine@3.20", Digest: map[string]string{"sha256": "deadbeef"}},
		{URI: "https://example.com/foo.tar", Digest: map[string]string{"sha256": "feed"}},
	}
	return p
}

func TestMakeBuildPlanHappyPath(t *testing.T) {
	s := testSubject(t)
	pred := testPredicate([]struct {
		id       string
		optional bool
	}{
		{id: "required"},
		{id: "optional", optional: true},
	}, nil)
	resolver, err := NewMaterialsResolver(nil)
	require.NoError(t, err)

	req := &BuildRequest{
		Targets:   []Target{{Subject: s, Predicate: pred}},
		Mode:      BuildModeMaterials,
		Materials: resolver,
		Secrets:   buildflags.Secrets{{ID: "required"}},
	}
	plan, err := MakeBuildPlan(req)
	require.NoError(t, err)
	require.Len(t, plan.Subjects, 1)
	require.Equal(t, s.Descriptor.Digest, plan.Subjects[0].Descriptor.Digest)
	require.Len(t, plan.Subjects[0].Materials, 2)
	require.Equal(t, "https://github.com/example/repo.git", plan.Subjects[0].BuildConfig.Context)
	require.Equal(t, "Dockerfile", plan.Subjects[0].BuildConfig.Filename)
	require.Equal(t, map[string]string{
		"cmdline": "docker/dockerfile:1.8",
		"source":  "docker/dockerfile:1.8",
	}, plan.Subjects[0].BuildConfig.FrontendAttrs)
	require.Equal(t, []PlanSecret{
		{ID: "optional", Optional: true},
		{ID: "required"},
	}, plan.Subjects[0].BuildConfig.Secrets)
	// First material is image-kind.
	require.Equal(t, "image", plan.Subjects[0].Materials[0].Kind)
	// Second material is http.
	require.Equal(t, "http", plan.Subjects[0].Materials[1].Kind)
	// JSON shape is stable.
	dt, err := json.Marshal(plan)
	require.NoError(t, err)
	require.NotContains(t, string(dt), `"inputRef":`)
	require.NotContains(t, string(dt), `"platform":"linux/amd64"`)
	require.NotContains(t, string(dt), `"predicateType":`)
	require.NotContains(t, string(dt), `"pins":`)
	require.NotContains(t, string(dt), `"replayMode":`)
	require.NotContains(t, string(dt), `"warnings":`)
	require.NotContains(t, string(dt), `"build-arg:`)
	require.NotContains(t, string(dt), `"label:`)
}

func TestMakeBuildPlanLocalContextRejected(t *testing.T) {
	s := testSubject(t)
	pred := testPredicate(nil, []string{"ctx"})
	req := &BuildRequest{
		Targets: []Target{{Subject: s, Predicate: pred}},
	}
	_, err := MakeBuildPlan(req)
	require.Error(t, err)
	var ulc *UnreplayableLocalContextError
	require.ErrorAs(t, err, &ulc)
}

func TestMakeBuildPlanExtraSecretRejected(t *testing.T) {
	s := testSubject(t)
	pred := testPredicate(nil, nil)
	req := &BuildRequest{
		Targets: []Target{{Subject: s, Predicate: pred}},
		Secrets: buildflags.Secrets{{ID: "rogue"}},
	}
	_, err := MakeBuildPlan(req)
	require.Error(t, err)
	var es *ExtraSecretError
	require.ErrorAs(t, err, &es)
	require.Equal(t, []string{"rogue"}, es.IDs)
}

func TestMakeBuildPlanMissingRecordedContextRejected(t *testing.T) {
	s := testSubject(t)
	pred := testPredicate(nil, nil)
	delete(pred.BuildDefinition.ExternalParameters.Request.Args, "context")
	pred.BuildDefinition.ExternalParameters.ConfigSource.URI = ""
	req := &BuildRequest{
		Targets: []Target{{Subject: s, Predicate: pred}},
	}
	_, err := MakeBuildPlan(req)
	require.EqualError(t, err, "predicate has no recorded build context; replay requires a remote-source build (git / https)")
}

func TestMakeSnapshotPlanHappyPath(t *testing.T) {
	fx := makeSnapshotFixture(t)

	req := &SnapshotRequest{
		Targets:          []Target{{Subject: fx.subject, Predicate: fx.predicate}},
		IncludeMaterials: true,
		Materials:        snapshotOverrideResolver(t, fx.httpURI, fx.httpBytes),
	}
	plan, err := MakeSnapshotPlan(context.Background(), nil, "", req)
	require.NoError(t, err)
	require.Len(t, plan, 1)
	require.Equal(t, fx.subject.Descriptor.Digest, plan[0].Subject.Digest)
	require.NotEmpty(t, plan[0].Materials)
	// Fixture has an http material only; non-image entries must not
	// carry a manifest-derived size.
	for _, m := range plan[0].Materials {
		if m.Kind != "image" {
			require.Zero(t, m.Size, "non-image materials must not report a size")
		}
	}
}

func TestMakeSnapshotPlanAttestationFileRejected(t *testing.T) {
	s := testSubject(t)
	s.kind = subjectKindAttestationFile
	pred := testPredicate(nil, nil)
	req := &SnapshotRequest{
		Targets: []Target{{Subject: s, Predicate: pred}},
	}
	_, err := MakeSnapshotPlan(context.Background(), nil, "", req)
	require.Error(t, err)
	var us *UnsupportedSubjectError
	require.ErrorAs(t, err, &us)
}
