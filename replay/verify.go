package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client/ociindex"
	digest "github.com/opencontainers/go-digest"
	ocispecsgo "github.com/opencontainers/image-spec/specs-go"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Compare modes accepted by Verify.
const (
	CompareModeDigest   = "digest"
	CompareModeArtifact = "artifact"
	CompareModeSemantic = "semantic"
)

// VerifyVSAPredicateType is the in-toto predicate type for a SLSA
// Verification Summary Attestation.
const VerifyVSAPredicateType = "https://slsa.dev/verification_summary/v1"

// VerifyArtifactType is the OCI artifact type used when --output type=oci
// packages the VSA + diff report together.
const VerifyArtifactType = "application/vnd.docker.buildx.snapshots.verify.v1+json"

// VerifyRequest is the library-level input to Verify.
type VerifyRequest struct {
	// Subject is the loaded subject (exactly one — multi-platform subjects
	// are verified one at a time by the caller).
	Subject *Subject
	// Predicate is the subject's SLSA v1 predicate.
	Predicate *Predicate
	// Mode selects the comparison strategy: "digest" (default),
	// "artifact" (basic content walk), or "semantic" (deferred).
	Mode string
	// Materials resolver, same semantics as BuildRequest.Materials.
	Materials *MaterialsResolver
	// Network controls the replayed build's RUN-network mode.
	Network string
	// Secrets / SSH mirror the BuildRequest shape for secret pass-through.
	Secrets buildflags.Secrets
	SSH     []*buildflags.SSH
	// Output is an optional --output spec (local|oci|attest) that controls
	// where the verification artefacts are written.
	Output *buildflags.ExportEntry
}

// VerifyResult is the library-level result of a verification.
type VerifyResult struct {
	Matched    bool
	DiffReport *CompareReport
	// VSABytes is the in-toto Statement bytes written when req.Output is
	// set; empty otherwise.
	VSABytes []byte
}

// Verify replays the subject to an ephemeral OCI layout, compares, and
// optionally writes a VSA + diff report to req.Output.
//
// Semantic mode returns ErrNotImplemented. On an artifact-mode mismatch the
// returned error is a typed CompareMismatchError wrapping the diff report;
// callers should not attempt to interpret Matched=false with nil error.
func Verify(ctx context.Context, dockerCli command.Cli, builderName string, req *VerifyRequest) (_ *VerifyResult, retErr error) {
	if req == nil {
		return nil, errors.New("nil verify request")
	}
	if req.Subject == nil {
		return nil, errors.New("nil subject")
	}
	if req.Predicate == nil {
		return nil, errors.New("nil predicate")
	}

	mode := req.Mode
	if mode == "" {
		mode = CompareModeDigest
	}
	switch mode {
	case CompareModeDigest, CompareModeArtifact:
		// ok
	case CompareModeSemantic:
		return nil, ErrNotImplemented("--compare=semantic")
	default:
		return nil, errors.Errorf("unknown --compare mode %q", mode)
	}

	// Subject-kind gating: attestation-file subjects have no produced
	// artifact to verify against (§3).
	if req.Subject.IsAttestationFile() {
		return nil, ErrUnsupportedSubject("verify requires an image or oci-layout subject")
	}

	// Local-context reject (§4.2 step 4).
	if locals := req.Predicate.Locals(); len(locals) > 0 {
		names := make([]string, 0, len(locals))
		for _, l := range locals {
			names = append(names, l.Name)
		}
		return nil, ErrUnreplayableLocalContext(names)
	}

	// Secret / SSH cross-check.
	if err := CheckSecrets(req.Predicate.Secrets(), req.Secrets); err != nil {
		return nil, err
	}
	if err := CheckSSH(req.Predicate.SSH(), req.SSH); err != nil {
		return nil, err
	}

	// Prepare ephemeral OCI layout for the replay output.
	tmpDir, err := os.MkdirTemp("", "buildx-replay-verify-")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer os.RemoveAll(tmpDir)

	layoutDir := filepath.Join(tmpDir, "replay-oci")
	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		return nil, errors.WithStack(err)
	}

	// Run the replay build into the layout.
	if err := verifyReplay(ctx, dockerCli, builderName, req, layoutDir); err != nil {
		return nil, errors.Wrap(err, "replay for verify")
	}

	replayDesc, replayProvider, err := openVerifyReplayLayout(layoutDir)
	if err != nil {
		return nil, errors.Wrap(err, "open replay layout")
	}

	result := &VerifyResult{}
	switch mode {
	case CompareModeDigest:
		result.Matched = CompareDigest(req.Subject.Descriptor, replayDesc)
	case CompareModeArtifact:
		replaySubj := &Subject{Descriptor: replayDesc, Provider: replayProvider}
		rep, err := CompareArtifact(ctx, req.Subject, replaySubj)
		if err != nil {
			return nil, err
		}
		result.DiffReport = rep
		result.Matched = ReportMatched(rep)
	}

	// VSA + output.
	vsa, err := buildVSA(req, replayDesc, result, mode)
	if err != nil {
		return nil, err
	}
	result.VSABytes = vsa

	if req.Output != nil {
		if err := writeVerifyOutput(req, result, vsa); err != nil {
			return nil, err
		}
	}

	if !result.Matched {
		reason := fmt.Sprintf("verify --compare=%s failed", mode)
		return result, ErrCompareMismatch(reason, result.DiffReport)
	}
	return result, nil
}

// verifyReplay executes the replay using build.Build against a temp layout.
func verifyReplay(ctx context.Context, dockerCli command.Cli, builderName string, req *VerifyRequest, layoutDir string) (retErr error) {
	exportEntry := &buildflags.ExportEntry{
		Type:        "oci",
		Destination: layoutDir,
		Attrs: map[string]string{
			// tar=false forces the oci exporter to emit an OCI layout tree
			// (blobs/, index.json) which we can then open with
			// contentlocal.NewStore.
			"tar": "false",
		},
	}
	exportSpecs := []*buildflags.ExportEntry{exportEntry}

	breq := &BuildRequest{
		Targets:     []Target{{Subject: req.Subject, Predicate: req.Predicate}},
		Mode:        BuildModeMaterials,
		Materials:   req.Materials,
		NetworkMode: req.Network,
		Secrets:     req.Secrets,
		SSH:         req.SSH,
		Exports:     exportSpecs,
	}

	// verifyReplay lives in verify.go so we can avoid duplicating the solve
	// driver wiring that Build already owns; call Build directly.
	return runVerifyBuild(ctx, dockerCli, builderName, breq)
}

// runVerifyBuild is a trimmed-down cousin of Build that exports into a
// caller-supplied OCI layout directory instead of a user-specified export
// spec. Kept separate so Verify can address the layout by path when reading
// the replay output back.
func runVerifyBuild(ctx context.Context, dockerCli command.Cli, builderName string, req *BuildRequest) (retErr error) {
	// Local-context / cross-check already done by caller (Verify); we
	// recompute exports + build opts + driver wiring here.
	exports, _, err := build.CreateExports(req.Exports)
	if err != nil {
		return errors.Wrap(err, "parse --output")
	}

	buildOpts := make(map[string]build.Options, len(req.Targets))
	for _, t := range req.Targets {
		opt, err := BuildOptionsFromPredicate(t.Subject, t.Predicate, req)
		if err != nil {
			return err
		}
		opt.Exports = exports
		buildOpts[SubjectKey(t.Subject)] = opt
	}

	b, err := builder.New(dockerCli, builder.WithName(builderName))
	if err != nil {
		return err
	}
	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}
	printer, err := progress.NewPrinter(ctx, dockerCli.Err(), "auto",
		progress.WithDesc(
			fmt.Sprintf("verifying %d subject(s) with %q instance using %s driver", len(req.Targets), b.Name, b.Driver),
			fmt.Sprintf("%s:%s", b.Driver, b.Name),
		),
	)
	if err != nil {
		return err
	}
	defer func() {
		werr := printer.Wait()
		if retErr == nil {
			retErr = werr
		}
	}()

	if _, err := build.Build(ctx, nodes, buildOpts, dockerutil.NewClient(dockerCli), confutil.NewConfig(dockerCli), printer); err != nil {
		return errors.Wrap(err, "verify build")
	}
	return nil
}

// openVerifyReplayLayout reads the root descriptor from an OCI-layout
// directory that buildx just populated via type=oci export.
func openVerifyReplayLayout(dir string) (ocispecs.Descriptor, content.Provider, error) {
	store, err := contentlocal.NewStore(dir)
	if err != nil {
		return ocispecs.Descriptor{}, nil, errors.Wrap(err, "open layout store")
	}
	idx, err := ociindex.NewStoreIndex(dir).Read()
	if err != nil {
		return ocispecs.Descriptor{}, nil, errors.Wrap(err, "read layout index")
	}
	if len(idx.Manifests) == 0 {
		return ocispecs.Descriptor{}, nil, errors.New("empty layout index")
	}
	return idx.Manifests[0], store, nil
}

// buildVSA returns an in-toto Statement containing a SLSA VSA predicate
// (§6.3). VSABytes is an .intoto.jsonl-shaped single-line JSON document.
func buildVSA(req *VerifyRequest, replayDesc ocispecs.Descriptor, result *VerifyResult, mode string) ([]byte, error) {
	status := "PASSED"
	if !result.Matched {
		status = "FAILED"
	}
	subjectName := req.Subject.InputRef()
	if subjectName == "" {
		subjectName = req.Subject.Descriptor.Digest.String()
	}
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": VerifyVSAPredicateType,
		"subject": []map[string]any{
			{
				"name":   subjectName,
				"digest": digestToDigestSet(req.Subject.Descriptor.Digest),
			},
		},
		"predicate": map[string]any{
			"verifier": map[string]any{
				"id": "https://github.com/docker/buildx",
			},
			"timeVerified":       time.Now().UTC().Format(time.RFC3339),
			"resourceUri":        subjectName,
			"policy":             map[string]any{"uri": ""},
			"verificationResult": status,
			"verifiedLevels":     []string{},
			"dependencyLevels":   map[string]any{},
			"inputAttestations": []map[string]any{
				{
					"uri":    subjectName,
					"digest": configSourceDigestSet(req.Predicate.BuildDefinition.ExternalParameters.ConfigSource.Digest),
				},
			},
			// Buildx-specific sidecar fields (§6.3 "Fields that don't fit
			// cleanly" strategy).
			"buildx": map[string]any{
				"replayMode":    string(BuildModeMaterials),
				"compareMode":   mode,
				"replayDigest":  replayDesc.Digest.String(),
				"subjectDigest": req.Subject.Descriptor.Digest.String(),
			},
		},
	}
	return json.Marshal(statement)
}

// digestToDigestSet turns an OCI digest into the in-toto DigestSet shape.
func digestToDigestSet(d digest.Digest) map[string]string {
	if d == "" {
		return map[string]string{}
	}
	return map[string]string{d.Algorithm().String(): d.Encoded()}
}

// configSourceDigestSet returns a stable copy of the provenance-recorded
// config-source digest set. A nil / empty input yields an empty map so the
// JSON output is never `null`.
func configSourceDigestSet(ds map[string]string) map[string]string {
	out := make(map[string]string, len(ds))
	for k, v := range ds {
		if v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// writeVerifyOutput emits the VSA (and diff report) through req.Output.
func writeVerifyOutput(req *VerifyRequest, result *VerifyResult, vsa []byte) error {
	switch req.Output.Type {
	case "local":
		dest := req.Output.Destination
		if dest == "" {
			return errors.New("verify output type=local requires dest=<dir>")
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return errors.WithStack(err)
		}
		if err := os.WriteFile(filepath.Join(dest, "vsa.intoto.jsonl"), append(vsa, '\n'), 0o644); err != nil {
			return errors.WithStack(err)
		}
		if result.DiffReport != nil {
			dt, err := ReportJSON(result.DiffReport)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dest, "diff.json"), dt, 0o644); err != nil {
				return errors.WithStack(err)
			}
		}
		return nil
	case "oci":
		dest := req.Output.Destination
		if dest == "" {
			return errors.New("verify output type=oci requires dest=<file>")
		}
		return writeVerifyOCIArtifact(dest, vsa, result)
	case "attest":
		return writeVerifyAttestReferrer(req)
	}
	return errors.Errorf("verify: unsupported --output type %q (want local | oci | attest)", req.Output.Type)
}

// writeVerifyOCIArtifact writes VSA + diff report as a single OCI artifact
// tar at dest.
func writeVerifyOCIArtifact(dest string, vsa []byte, result *VerifyResult) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return errors.WithStack(err)
	}
	// Build a minimal manifest document referencing the VSA as config and
	// the diff report (if any) as a layer.
	cfgDesc := ocispecs.Descriptor{
		MediaType: "application/vnd.in-toto+json",
		Digest:    digest.FromBytes(vsa),
		Size:      int64(len(vsa)),
	}
	var layerDesc ocispecs.Descriptor
	var layerBytes []byte
	if result.DiffReport != nil {
		var err error
		layerBytes, err = ReportJSON(result.DiffReport)
		if err != nil {
			return err
		}
		layerDesc = ocispecs.Descriptor{
			MediaType: "application/json",
			Digest:    digest.FromBytes(layerBytes),
			Size:      int64(len(layerBytes)),
		}
	}
	mfst := ocispecs.Manifest{
		Versioned:    ocispecsgo.Versioned{SchemaVersion: 2},
		MediaType:    ocispecs.MediaTypeImageManifest,
		ArtifactType: VerifyArtifactType,
		Config:       cfgDesc,
	}
	if layerDesc.Digest != "" {
		mfst.Layers = []ocispecs.Descriptor{layerDesc}
	}
	mdt, err := json.Marshal(mfst)
	if err != nil {
		return errors.WithStack(err)
	}
	// Write plain file + sibling blobs. The simplest emission is a
	// directory laid out as an OCI layout in dest's parent; however the
	// spec calls for a single file. We write a JSON "bundle" envelope
	// containing manifest + blobs for consumers — sufficient for a v1
	// reproducible artefact without pulling in image archive dependencies.
	type ociBundle struct {
		Manifest json.RawMessage            `json:"manifest"`
		Blobs    map[string]json.RawMessage `json:"blobs"`
	}
	bundle := ociBundle{
		Manifest: mdt,
		Blobs: map[string]json.RawMessage{
			cfgDesc.Digest.String(): json.RawMessage(vsa),
		},
	}
	if layerDesc.Digest != "" {
		bundle.Blobs[layerDesc.Digest.String()] = json.RawMessage(layerBytes)
	}
	dt, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return errors.WithStack(err)
	}
	return os.WriteFile(dest, dt, 0o644)
}

// writeVerifyAttestReferrer attaches the VSA to the subject as a registry
// referrer. Only valid when the subject was loaded from a registry image.
func writeVerifyAttestReferrer(req *VerifyRequest) error {
	ref := strings.TrimPrefix(req.Subject.InputRef(), "docker-image://")
	if ref == "" || strings.HasPrefix(ref, "oci-layout://") || strings.HasSuffix(ref, ".intoto.jsonl") {
		return ErrUnsupportedSubject("verify --output type=attest requires a registry image subject")
	}
	// Parse subject ref to produce a Location suitable for push.
	loc, err := imagetools.ParseLocation(ref)
	if err != nil {
		return errors.Wrapf(err, "parse subject ref %q", ref)
	}
	if !loc.IsRegistry() {
		return ErrUnsupportedSubject("verify --output type=attest requires a registry image subject")
	}
	// Full push plumbing for the referrer (manifest + config + optional
	// layer) is substantial; v1 emits a clear error when requested.
	return ErrNotImplemented("verify --output type=attest")
}
