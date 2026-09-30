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

// defaultFrontend matches the frontend buildx uses when none is recorded on
// the request.
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
// keys stripped. Returns a fresh map so callers can mutate it.
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

// TargetPlatform returns the target platform recorded in provenance, falling
// back to the build's LLB for older attestations that lack the field.
func (p *Predicate) TargetPlatform() (*ocispecs.Platform, bool) {
	params := p.BuildDefinition.InternalParameters
	if params.TargetPlatform != "" {
		platform, err := platforms.Parse(params.TargetPlatform)
		if err != nil {
			return nil, false
		}
		norm := platforms.Normalize(platform)
		return &norm, true
	}
	return p.FallbackTargetPlatform()
}

// FallbackTargetPlatform infers the target platform from the build's LLB for
// older provenance that does not record targetPlatform. BuildKit injects
// TARGETPLATFORM into Dockerfile exec environments. A single provenance
// statement must agree on that value; mixed or malformed values are not safe
// to use as an implicit replay target.
func (p *Predicate) FallbackTargetPlatform() (*ocispecs.Platform, bool) {
	buildConfig := p.BuildDefinition.InternalParameters.BuildConfig
	if buildConfig == nil {
		return nil, false
	}
	var target *ocispecs.Platform
	for _, step := range buildConfig.Definition {
		if step.Op == nil || step.Op.GetExec() == nil || step.Op.GetExec().Meta == nil {
			continue
		}
		for _, env := range step.Op.GetExec().Meta.Env {
			value, ok := strings.CutPrefix(env, "TARGETPLATFORM=")
			if !ok || value == "" {
				continue
			}
			platform, err := platforms.Parse(value)
			if err != nil {
				return nil, false
			}
			norm := platforms.Normalize(platform)
			if target != nil && platforms.Format(*target) != platforms.Format(norm) {
				return nil, false
			}
			target = &norm
		}
	}
	return target, target != nil
}

// ResolvedDependencies returns every material recorded on the predicate.
// Classification by URI scheme is left to the caller (see MaterialsResolver).
func (p *Predicate) ResolvedDependencies() []slsa1.ResourceDescriptor {
	return p.BuildDefinition.ResolvedDependencies
}

// IsMinMode reports whether the provenance was recorded with mode=min. Only
// mode=max records the build definition. mode=min also drops build arguments,
// labels, secrets and SSH from the recorded request, so the original build
// cannot be reconstructed from it.
func (p *Predicate) IsMinMode() bool {
	bc := p.BuildDefinition.InternalParameters.BuildConfig
	return bc == nil || len(bc.Definition) == 0
}
