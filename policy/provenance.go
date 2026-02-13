package policy

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
)

const predicateTypeAnnotation = "in-toto.io/predicate-type"

var resolveProvenanceAttestations = []string{
	slsa02.PredicateSLSAProvenance,
	slsa1.PredicateSLSAProvenance,
}

type inTotoStatement struct {
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

func parseProvenance(ac *gwpb.AttestationChain) (*ImageProvenance, error) {
	if ac == nil || len(ac.Blobs) == 0 {
		return nil, nil
	}

	// Prefer blobs that explicitly declare a provenance predicate type.
	for _, b := range ac.Blobs {
		if b == nil || b.Descriptor_ == nil || len(b.Data) == 0 {
			continue
		}
		pt := b.Descriptor_.Annotations[predicateTypeAnnotation]
		if pt == "" {
			continue
		}
		if !slices.Contains(resolveProvenanceAttestations, pt) {
			continue
		}
		prv, err := parseProvenanceBlob(b.Data, pt)
		if err != nil {
			return nil, err
		}
		if prv != nil {
			return prv, nil
		}
	}

	return nil, nil
}

func parseProvenanceBlob(dt []byte, pt string) (*ImageProvenance, error) {
	var stmt inTotoStatement
	if err := json.Unmarshal(dt, &stmt); err != nil || len(stmt.Predicate) == 0 {
		return nil, nil
	}
	if stmt.PredicateType != "" && stmt.PredicateType != pt {
		return nil, nil
	}

	switch pt {
	case slsa1.PredicateSLSAProvenance:
		return parseSLSA1Provenance(stmt.Predicate)
	case slsa02.PredicateSLSAProvenance:
		return parseSLSA02Provenance(stmt.Predicate)
	}
	return nil, nil
}

func parseSLSA1Provenance(dt []byte) (*ImageProvenance, error) {
	var pred provenancetypes.ProvenancePredicateSLSA1
	if err := json.Unmarshal(dt, &pred); err != nil {
		return nil, nil
	}
	if pred.BuildDefinition.BuildType == "" && pred.RunDetails.Builder.ID == "" {
		return nil, nil
	}
	prv := &ImageProvenance{
		PredicateType: slsa1.PredicateSLSAProvenance,
		BuildType:     pred.BuildDefinition.BuildType,
		BuilderID:     pred.RunDetails.Builder.ID,
		ConfigSource: &ImageProvenanceConfigSource{
			URI:    pred.BuildDefinition.ExternalParameters.ConfigSource.URI,
			Digest: pred.BuildDefinition.ExternalParameters.ConfigSource.Digest,
			Path:   pred.BuildDefinition.ExternalParameters.ConfigSource.Path,
		},
		Frontend:  pred.BuildDefinition.ExternalParameters.Request.Frontend,
		BuildArgs: extractBuildArgs(pred.BuildDefinition.ExternalParameters.Request.Args),
		RawArgs:   pred.BuildDefinition.ExternalParameters.Request.Args,
	}

	if md := pred.RunDetails.Metadata; md != nil {
		prv.InvocationID = md.InvocationID
		prv.StartedOn = formatProvenanceTime(md.StartedOn)
		prv.FinishedOn = formatProvenanceTime(md.FinishedOn)
		prv.Reproducible = boolPtr(md.Reproducible)
		prv.Hermetic = boolPtr(md.Hermetic)
		prv.Completeness = &ImageProvenanceCompleteness{
			Parameters: boolPtr(md.Completeness.Request),
			Materials:  boolPtr(md.Completeness.ResolvedDependencies),
		}
	}

	return prv, nil
}

func parseSLSA02Provenance(dt []byte) (*ImageProvenance, error) {
	var pred provenancetypes.ProvenancePredicateSLSA02
	if err := json.Unmarshal(dt, &pred); err != nil {
		return nil, nil
	}
	if pred.BuildType == "" && pred.Builder.ID == "" {
		return nil, nil
	}
	prv := &ImageProvenance{
		PredicateType: slsa02.PredicateSLSAProvenance,
		BuildType:     pred.BuildType,
		BuilderID:     pred.Builder.ID,
		ConfigSource: &ImageProvenanceConfigSource{
			URI:    pred.Invocation.ConfigSource.URI,
			Digest: pred.Invocation.ConfigSource.Digest,
			Path:   pred.Invocation.ConfigSource.EntryPoint,
		},
		Frontend:  pred.Invocation.Parameters.Frontend,
		BuildArgs: extractBuildArgs(pred.Invocation.Parameters.Args),
		RawArgs:   pred.Invocation.Parameters.Args,
	}

	if md := pred.Metadata; md != nil {
		prv.InvocationID = md.BuildInvocationID
		prv.StartedOn = formatProvenanceTime(md.BuildStartedOn)
		prv.FinishedOn = formatProvenanceTime(md.BuildFinishedOn)
		prv.Reproducible = boolPtr(md.Reproducible)
		prv.Hermetic = boolPtr(md.Hermetic)
		prv.Completeness = &ImageProvenanceCompleteness{
			Parameters:  boolPtr(md.Completeness.Parameters),
			Environment: boolPtr(md.Completeness.Environment),
			Materials:   boolPtr(md.Completeness.Materials),
		}
	}

	return prv, nil
}

func boolPtr(v bool) *bool {
	return &v
}

func formatProvenanceTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func extractBuildArgs(args map[string]string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	const prefix = "build-arg:"
	out := make(map[string]string)
	for k, v := range args {
		if name, ok := strings.CutPrefix(k, prefix); ok && name != "" {
			out[name] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
