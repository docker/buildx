package replay

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyRejectsSemantic(t *testing.T) {
	req := &VerifyRequest{
		Subject:   &Subject{},
		Predicate: &Predicate{},
		Mode:      CompareModeSemantic,
	}
	_, err := Verify(context.Background(), nil, "", req)
	require.Error(t, err)
	var nie *NotImplementedError
	require.ErrorAs(t, err, &nie)
	require.Equal(t, "--compare=semantic", nie.Feature)
}

func TestVerifyRejectsUnknownMode(t *testing.T) {
	req := &VerifyRequest{
		Subject:   &Subject{},
		Predicate: &Predicate{},
		Mode:      "bogus",
	}
	_, err := Verify(context.Background(), nil, "", req)
	require.Error(t, err)
}

func TestVerifyRejectsAttestationFileSubject(t *testing.T) {
	s := &Subject{kind: subjectKindAttestationFile}
	req := &VerifyRequest{
		Subject:   s,
		Predicate: &Predicate{},
		Mode:      CompareModeDigest,
	}
	_, err := Verify(context.Background(), nil, "", req)
	require.Error(t, err)
	var us *UnsupportedSubjectError
	require.ErrorAs(t, err, &us)
}

func TestVerifyRejectsLocalContext(t *testing.T) {
	pred := testPredicate(nil, []string{"ctx"})
	req := &VerifyRequest{
		Subject:   testSubject(t),
		Predicate: pred,
		Mode:      CompareModeDigest,
	}
	_, err := Verify(context.Background(), nil, "", req)
	require.Error(t, err)
	var ulc *UnreplayableLocalContextError
	require.ErrorAs(t, err, &ulc)
}

func TestBuildVSASchema(t *testing.T) {
	// Exercise the VSA builder directly — it's deterministic given inputs
	// and does not require a live daemon.
	s := testSubject(t)
	s.inputRef = "docker-image://example.test/app:v1"

	pred := testPredicate(nil, nil)
	pred.BuildDefinition.ExternalParameters.ConfigSource.Digest = map[string]string{"sha256": "cafef00d"}

	req := &VerifyRequest{
		Subject:   s,
		Predicate: pred,
	}
	result := &VerifyResult{Matched: true}
	dt, err := buildVSA(req, s.Descriptor, result, CompareModeDigest)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(dt, &parsed))

	require.Equal(t, "https://in-toto.io/Statement/v1", parsed["_type"])
	require.Equal(t, VerifyVSAPredicateType, parsed["predicateType"])
	subjects := parsed["subject"].([]any)
	require.Len(t, subjects, 1)
	require.Equal(t, "docker-image://example.test/app:v1", subjects[0].(map[string]any)["name"])

	pred0 := parsed["predicate"].(map[string]any)
	require.Equal(t, "PASSED", pred0["verificationResult"])
	verifier := pred0["verifier"].(map[string]any)
	require.Equal(t, "https://github.com/docker/buildx", verifier["id"])
	buildxSide := pred0["buildx"].(map[string]any)
	require.Equal(t, string(CompareModeDigest), buildxSide["compareMode"])
}

func TestBuildVSAFailedStatus(t *testing.T) {
	s := testSubject(t)
	pred := testPredicate(nil, nil)
	req := &VerifyRequest{Subject: s, Predicate: pred}
	result := &VerifyResult{Matched: false}
	dt, err := buildVSA(req, s.Descriptor, result, CompareModeArtifact)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(dt, &parsed))
	pred0 := parsed["predicate"].(map[string]any)
	require.Equal(t, "FAILED", pred0["verificationResult"])
}
