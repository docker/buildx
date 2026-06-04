package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	"github.com/moby/buildkit/client"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/purl"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/package-url/packageurl-go"
	"github.com/pkg/errors"
	"github.com/tonistiigi/go-csvvalue"
)

// BuildPlan is the JSON-serializable dry-run payload for `replay build`.
// Field names are stable and consumed by tests / tooling.
type BuildPlan struct {
	// Subjects is one SubjectBuildPlan per replay target.
	Subjects []SubjectBuildPlan `json:"subjects"`
}

// SubjectBuildPlan is the per-subject build-mode dry-run plan.
type SubjectBuildPlan struct {
	// Descriptor is the subject descriptor (digest + mediaType + size).
	Descriptor ocispecs.Descriptor `json:"descriptor"`
	// BuildConfig summarises the solve parameters replay would use.
	BuildConfig BuildPlanConfig `json:"buildConfig"`
	// Materials lists the resolved provenance materials.
	Materials []PlanMaterial `json:"materials"`
}

// BuildPlanConfig mirrors the build.Options fields replay derives from the
// predicate — enough for a user to eyeball that the replay will run as
// expected.
type BuildPlanConfig struct {
	Frontend      string            `json:"frontend"`
	FrontendAttrs map[string]string `json:"frontendAttrs,omitempty"`
	Context       string            `json:"context,omitempty"`
	Filename      string            `json:"filename,omitempty"`
	Target        string            `json:"target,omitempty"`
	BuildArgs     map[string]string `json:"buildArgs,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	NoCache       bool              `json:"noCache,omitempty"`
	NoCacheFilter []string          `json:"noCacheFilter,omitempty"`
	Secrets       []PlanSecret      `json:"secrets,omitempty"`
	SSH           []string          `json:"ssh,omitempty"`
	NetworkMode   string            `json:"networkMode,omitempty"`
	Exports       []string          `json:"exports,omitempty"`
}

// PlanMaterial describes one provenance material. Different dry-run modes
// populate different subsets of the fields, but the JSON shape stays stable.
type PlanMaterial struct {
	URI string `json:"uri,omitempty"`
	// Platform is populated for image materials only — either parsed
	// from the purl `?platform=` qualifier or, when the URI doesn't
	// carry one, from the predicate's builder platform.
	Platform *ocispecs.Platform `json:"platform,omitempty"`
	Digest   string             `json:"digest,omitempty"`
	// Kind is one of: "image", "image-blob" (container-blob), "http",
	// "git", or "unknown".
	Kind string `json:"kind"`
	// Included reports whether `replay snapshot` would copy this
	// material's bytes into the snapshot.
	Included bool `json:"included,omitempty"`
	// Size is the total byte size this material contributes to the
	// snapshot — the root index plus the platform-matched manifest
	// chain (config + all layer descriptor sizes). Only populated for
	// image materials during snapshot dry-run; computed from manifest
	// metadata alone (no layer bodies are fetched).
	Size int64 `json:"size,omitempty"`
}

// PlanSecret describes one declared secret plus whether it is optional.
type PlanSecret struct {
	ID       string `json:"id"`
	Optional bool   `json:"optional,omitempty"`
}

// SnapshotPlan is the JSON-serializable dry-run payload for
// `replay snapshot` — one entry per snapshot target.
type SnapshotPlan []SnapshotPlanTarget

// SnapshotPlanTarget is the per-subject snapshot dry-run plan.
type SnapshotPlanTarget struct {
	// Subject is the subject descriptor (already carries platform).
	Subject ocispecs.Descriptor `json:"subject"`
	// Materials lists each recorded material and whether the snapshot
	// would include its content.
	Materials []PlanMaterial `json:"materials"`
}

// MakeBuildPlan constructs the dry-run plan for a BuildRequest.
// Any fatal up-front condition (local-context, extra/missing secrets or ssh,
// unknown mode) surfaces as the same typed error the real build would
// produce — dry-run is a reliable pre-flight.
func MakeBuildPlan(req *BuildRequest) (*BuildPlan, error) {
	if req == nil {
		return nil, errors.New("nil build request")
	}
	if len(req.Targets) == 0 {
		return nil, errors.New("no targets to replay")
	}
	if req.Mode == BuildModeLLB {
		return nil, ErrNotImplemented("llb replay mode")
	}

	plan := &BuildPlan{Subjects: make([]SubjectBuildPlan, 0, len(req.Targets))}

	for _, t := range req.Targets {
		if t.Subject == nil || t.Predicate == nil {
			return nil, errors.New("target has nil subject or predicate")
		}
		// Same local-context check as Build (§4.2 step 4).
		if locals := t.Predicate.Locals(); len(locals) > 0 {
			names := make([]string, 0, len(locals))
			for _, l := range locals {
				names = append(names, l.Name)
			}
			return nil, ErrUnreplayableLocalContext(names)
		}
		if err := CheckSecrets(t.Predicate.Secrets(), req.Secrets); err != nil {
			return nil, err
		}
		if err := CheckSSH(t.Predicate.SSH(), req.SSH); err != nil {
			return nil, err
		}
		if _, err := BuildOptionsFromPredicate(t.Subject, t.Predicate, req); err != nil {
			return nil, err
		}

		plan.Subjects = append(plan.Subjects, subjectBuildPlan(t.Subject, t.Predicate, req))
	}
	return plan, nil
}

// MakeSnapshotPlan constructs the dry-run plan for a SnapshotRequest.
// For each image material, the root index + platform-matched manifest
// bodies are fetched so their descriptor sizes can be summed — layer
// bodies are not fetched.
func MakeSnapshotPlan(ctx context.Context, dockerCli command.Cli, builderName string, req *SnapshotRequest) (SnapshotPlan, error) {
	if req == nil {
		return nil, errors.New("nil snapshot request")
	}
	if len(req.Targets) == 0 {
		return nil, errors.New("no targets to snapshot")
	}

	// Register ref-key prefixes so MakeRefKey in containerd does not log
	// warnings while we fetch manifest bodies.
	ctx = withMediaTypeKeyPrefix(ctx)

	// Lazily-constructed registry resolver for image materials, shared
	// across targets — mirrors the real-run setup.
	var registryResolver *imagetools.Resolver
	lazyResolver := func() (*imagetools.Resolver, error) {
		if registryResolver != nil {
			return registryResolver, nil
		}
		if dockerCli == nil {
			registryResolver = imagetools.New(imagetools.Opt{})
			return registryResolver, nil
		}
		b, err := builder.New(dockerCli, builder.WithName(builderName))
		if err != nil {
			return nil, err
		}
		imgOpt, err := b.ImageOpt()
		if err != nil {
			return nil, err
		}
		registryResolver = imagetools.New(imgOpt)
		return registryResolver, nil
	}

	plan := make(SnapshotPlan, 0, len(req.Targets))
	var pwlog progress.Logger = func(*client.SolveStatus) {}
	if req.Progress != nil {
		pwlog = req.Progress.Write
	}

	for ti, t := range req.Targets {
		s, pred := t.Subject, t.Predicate
		if s == nil || pred == nil {
			return nil, errors.New("target has nil subject or predicate")
		}
		if s.IsAttestationFile() {
			return nil, ErrUnsupportedSubject("snapshot requires an image or oci-layout subject")
		}
		if s.AttestationManifest().Digest == "" {
			return nil, ErrNoProvenance(s.InputRef())
		}

		var mats []PlanMaterial
		targetName := fmt.Sprintf("[%d/%d] snapshot %s", ti+1, len(req.Targets), snapshotTargetLabel(s))
		err := progress.Wrap(targetName, pwlog, func(sub progress.SubLogger) error {
			var err error
			mats, err = planMaterials(ctx, s, pred, req, lazyResolver, sub)
			return err
		})
		if err != nil {
			return nil, err
		}
		plan = append(plan, SnapshotPlanTarget{
			Subject:   s.Descriptor,
			Materials: mats,
		})
	}
	return plan, nil
}

// planMaterials builds the material list for the dry-run plan. For image
// materials it resolves the root index + picks the platform-matched
// manifest and sums every descriptor's size — layer bytes are not
// fetched.
func planMaterials(ctx context.Context, s *Subject, pred *Predicate, req *SnapshotRequest, lazyResolver func() (*imagetools.Resolver, error), sub progress.SubLogger) ([]PlanMaterial, error) {
	builder := pred.BuilderPlatform()
	var out []PlanMaterial
	for _, m := range pred.ResolvedDependencies() {
		entry := materialPlan(m, builder)
		kind := classifyMaterial(m)
		entry.Included = req.IncludeMaterials && kind != materialKindGit && kind != materialKindUnknown
		if kind == materialKindImage {
			if req.IncludeMaterials {
				var size int64
				err := sub.Wrap(fmt.Sprintf("plan image material %s", entry.URI), func() error {
					var err error
					size, err = imageMaterialSize(ctx, req.Materials, lazyResolver, m, preferredDigest(m.Digest), s.Descriptor.Platform, pred.BuilderPlatform())
					return err
				})
				if err != nil {
					return nil, errors.Wrapf(err, "size image material %s", m.URI)
				}
				entry.Size = size
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// imageMaterialSize fetches only the manifests needed to size an image
// material: root index → platform-matched manifest → its config + layers.
// Returns the sum of every descriptor's declared size. Layer bodies are
// never fetched.
func imageMaterialSize(ctx context.Context, resolver *MaterialsResolver, lazyResolver func() (*imagetools.Resolver, error), m slsa1.ResourceDescriptor, rootDgst digest.Digest, subjectPlat *ocispecs.Platform, builderPlat ocispecs.Platform) (int64, error) {
	rootDesc, provider, err := resolveImageMaterial(ctx, resolver, lazyResolver, m, rootDgst, WithPlatform(subjectPlat), WithBuilderPlatform(builderPlat))
	if err != nil {
		return 0, err
	}
	total := rootDesc.Size

	platDesc, err := pickPlatformChild(ctx, provider, rootDesc, subjectPlat, builderPlat)
	if err != nil {
		return 0, err
	}
	// pickPlatformChild may return rootDesc unchanged for single-platform
	// images; avoid double-counting.
	if platDesc.Digest != rootDesc.Digest {
		total += platDesc.Size
	}

	mfstData, err := content.ReadBlob(ctx, provider, platDesc)
	if err != nil {
		return 0, errors.Wrapf(err, "read platform manifest %s", platDesc.Digest)
	}
	var mfst ocispecs.Manifest
	if err := json.Unmarshal(mfstData, &mfst); err != nil {
		return 0, errors.Wrapf(err, "parse platform manifest %s", platDesc.Digest)
	}
	total += mfst.Config.Size
	for _, l := range mfst.Layers {
		total += l.Size
	}
	return total, nil
}

// stripImagePurlQualifiers removes the `digest` and `platform` purl
// qualifiers from an image material URI — those values are already
// reported as separate fields on the plan entry so carrying them in the
// URI is pure noise. Falls back to the original URI on parse failure.
func stripImagePurlQualifiers(uri string) string {
	p, err := packageurl.FromString(uri)
	if err != nil {
		return uri
	}
	kept := p.Qualifiers[:0]
	for _, q := range p.Qualifiers {
		switch q.Key {
		case "digest", "platform":
			continue
		}
		kept = append(kept, q)
	}
	p.Qualifiers = kept
	return p.ToString()
}

func subjectBuildPlan(s *Subject, pred *Predicate, req *BuildRequest) SubjectBuildPlan {
	attrs := pred.FrontendAttrs()
	cfgSrc := pred.ConfigSource()
	contextPath := cfgSrc.URI
	if contextPath == "" {
		contextPath = attrs["context"]
	}
	dockerfilePath := cfgSrc.Path
	if dockerfilePath == "" {
		dockerfilePath = attrs["filename"]
	}

	cfg := BuildPlanConfig{
		Frontend:      pred.Frontend(),
		FrontendAttrs: frontendAttrSummary(attrs),
		Context:       contextPath,
		Filename:      dockerfilePath,
		Target:        attrs["target"],
		BuildArgs:     collectPrefixed(attrs, "build-arg:"),
		Labels:        collectPrefixed(attrs, "label:"),
		Secrets:       planSecrets(pred.Secrets()),
		SSH:           sshIDs(pred.SSH()),
		NetworkMode:   networkModeForReplay(req.NetworkMode),
		Exports:       exportSummaries(req.Exports),
	}
	if v, ok := attrs["no-cache"]; ok {
		if v == "" {
			cfg.NoCache = true
		} else if fields, err := csvvalue.Fields(v, nil); err == nil {
			cfg.NoCacheFilter = fields
		}
	}

	// Materials summary.
	mats := make([]PlanMaterial, 0, len(pred.ResolvedDependencies()))
	for _, m := range pred.ResolvedDependencies() {
		mats = append(mats, materialPlan(m, pred.BuilderPlatform()))
	}

	plan := SubjectBuildPlan{
		Descriptor:  s.Descriptor,
		BuildConfig: cfg,
		Materials:   mats,
	}
	return plan
}

func frontendAttrSummary(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, 2)
	for _, key := range []string{"source", "cmdline"} {
		if v, ok := attrs[key]; ok && v != "" {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// materialPlan builds the shared dry-run material summary shape.
func materialPlan(m slsa1.ResourceDescriptor, builder ocispecs.Platform) PlanMaterial {
	kind := classifyMaterial(m)
	pm := PlanMaterial{
		URI:  m.URI,
		Kind: materialKindString(kind),
	}
	d := preferredDigest(m.Digest)
	if d != "" {
		pm.Digest = d.String()
	}
	if kind == materialKindImage {
		if _, p, err := purl.PURLToRef(m.URI); err == nil && p != nil {
			pm.Platform = p
		} else {
			b := builder
			pm.Platform = &b
		}
		pm.URI = stripImagePurlQualifiers(m.URI)
	}
	return pm
}

// materialKindString returns the stable JSON kind tag for a material kind.
func materialKindString(k materialKind) string {
	switch k {
	case materialKindImage:
		return "image"
	case materialKindContainerBlob:
		return "image-blob"
	case materialKindHTTP:
		return "http"
	case materialKindGit:
		return "git"
	}
	return "unknown"
}

// planSecrets returns the sorted unique declared secrets, preserving whether
// any declaration of the secret marked it optional.
func planSecrets(secrets []*provenancetypes.Secret) []PlanSecret {
	seen := map[string]bool{}
	for _, s := range secrets {
		if s == nil || s.ID == "" {
			continue
		}
		seen[s.ID] = seen[s.ID] || s.Optional
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]PlanSecret, 0, len(ids))
	for _, id := range ids {
		out = append(out, PlanSecret{ID: id, Optional: seen[id]})
	}
	return out
}

// sshIDs returns the sorted unique IDs of declared SSH entries.
func sshIDs(entries []*provenancetypes.SSH) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range entries {
		if s == nil || s.ID == "" {
			continue
		}
		if _, ok := seen[s.ID]; ok {
			continue
		}
		seen[s.ID] = struct{}{}
		out = append(out, s.ID)
	}
	sort.Strings(out)
	return out
}

// exportSummaries renders --output specs into a short "type=..." list for
// dry-run JSON output.
func exportSummaries(exports []*buildflags.ExportEntry) []string {
	out := make([]string, 0, len(exports))
	for _, e := range exports {
		if e == nil {
			continue
		}
		s := "type=" + e.Type
		if e.Destination != "" {
			s += ",dest=" + e.Destination
		}
		if name, ok := e.Attrs["name"]; ok && name != "" {
			s += ",name=" + name
		}
		out = append(out, s)
	}
	return out
}
