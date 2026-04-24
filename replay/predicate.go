package replay

import (
	"strings"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/policy"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

// Predicate is a named type over ProvenancePredicateSLSA1 so replay code can
// attach accessors without copying or wrapping. The receiver is never nil:
// callers must have obtained a non-nil *Predicate from Subject.Predicate.
type Predicate provenancetypes.ProvenancePredicateSLSA1

// defaultFrontend matches BuildKit's default when no frontend is recorded on
// the request (see build/opt.go:309 — dockerfile.v0).
const defaultFrontend = "dockerfile.v0"

// Frontend returns the frontend id recorded on the predicate, falling back
// to dockerfile.v0 when the predicate does not record one.
func (p *Predicate) Frontend() string {
	if f := p.BuildDefinition.ExternalParameters.Request.Frontend; f != "" {
		return f
	}
	return defaultFrontend
}

// FrontendAttrs returns the recorded frontend attrs with attestation-related
// keys stripped (see Attests for those). Returns a fresh map so callers can
// mutate it.
func (p *Predicate) FrontendAttrs() map[string]string {
	src := p.BuildDefinition.ExternalParameters.Request.Args
	out := make(map[string]string, len(src))
	for k, v := range src {
		if strings.HasPrefix(k, "attest:") {
			continue
		}
		out[k] = v
	}
	return out
}

// Attests returns the recorded attestation-related frontend attrs as the
// map shape consumed by build.Options.Attests: key is the attestation type
// (the text after "attest:"), value is the recorded attr payload.
func (p *Predicate) Attests() map[string]*string {
	src := p.BuildDefinition.ExternalParameters.Request.Args
	out := map[string]*string{}
	for k, v := range src {
		name, ok := strings.CutPrefix(k, "attest:")
		if !ok {
			continue
		}
		vv := v
		out[name] = &vv
	}
	return out
}

// ConfigSource returns the configSource descriptor recorded on the predicate.
func (p *Predicate) ConfigSource() provenancetypes.ProvenanceConfigSourceSLSA1 {
	return p.BuildDefinition.ExternalParameters.ConfigSource
}

// Secrets returns the declared secrets from the predicate's request.
func (p *Predicate) Secrets() []*provenancetypes.Secret {
	return p.BuildDefinition.ExternalParameters.Request.Secrets
}

// SSH returns the declared SSH entries from the predicate's request.
func (p *Predicate) SSH() []*provenancetypes.SSH {
	return p.BuildDefinition.ExternalParameters.Request.SSH
}

// Locals returns the local-context sources recorded on the predicate. A
// non-empty result should cause replay to fail with
// UnreplayableLocalContextError.
func (p *Predicate) Locals() []*provenancetypes.LocalSource {
	return p.BuildDefinition.ExternalParameters.Request.Locals
}

// BuilderPlatform returns the platform the original builder ran on, parsed
// from InternalParameters.builderPlatform. Falls back to the runtime host
// platform when the field is missing or malformed.
func (p *Predicate) BuilderPlatform() ocispecs.Platform {
	if plat, ok := p.RecordedBuilderPlatform(); ok {
		return *plat
	}
	return platforms.DefaultSpec()
}

// RecordedBuilderPlatform returns the platform recorded in
// InternalParameters.builderPlatform when present and valid.
func (p *Predicate) RecordedBuilderPlatform() (*ocispecs.Platform, bool) {
	s := p.BuildDefinition.InternalParameters.BuilderPlatform
	if s == "" {
		return nil, false
	}
	plat, err := platforms.Parse(s)
	if err != nil {
		return nil, false
	}
	norm := platforms.Normalize(plat)
	return &norm, true
}

// DefaultPlatform returns the effective provenance default platform for
// resolving host-side image sources during replay. It prefers the recorded
// platform-qualified image materials when they all agree, and otherwise
// falls back to the recorded builderPlatform field.
func (p *Predicate) DefaultPlatform() (*ocispecs.Platform, bool) {
	var inferred *ocispecs.Platform
	for _, m := range p.ResolvedDependencies() {
		_, mp, err := policy.ParseSLSAMaterial(m)
		if err != nil || mp == nil {
			continue
		}
		norm := platforms.Normalize(*mp)
		if inferred == nil {
			inferred = &norm
			continue
		}
		if platforms.Format(*inferred) != platforms.Format(norm) {
			return nil, false
		}
	}
	if inferred != nil {
		return inferred, true
	}
	if plat, ok := p.RecordedBuilderPlatform(); ok {
		return plat, true
	}
	return nil, false
}

// ResolvedDependencies returns every material recorded on the predicate.
// Classification by URI scheme is left to the caller (see MaterialsResolver).
func (p *Predicate) ResolvedDependencies() []slsa1.ResourceDescriptor {
	return p.BuildDefinition.ResolvedDependencies
}

// HasBuildDefinition reports whether the predicate carries a non-empty LLB
// substrate (required by --replay-mode=llb).
func (p *Predicate) HasBuildDefinition() bool {
	bc := p.BuildDefinition.InternalParameters.BuildConfig
	return bc != nil && len(bc.Definition) > 0
}
