package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	contentlocal "github.com/containerd/containerd/v2/plugins/content/local"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client/ociindex"
	"github.com/moby/buildkit/util/progress/progressui"
	digest "github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// Compare modes accepted by Verify.
const (
	CompareModeDigest   = "digest"
	CompareModeArtifact = "artifact"
)

// VerifyVSAPredicateType is the in-toto predicate type for a SLSA
// Verification Summary Attestation.
const VerifyVSAPredicateType = "https://slsa.dev/verification_summary/v1"

// VerifyRequest is the library-level input to Verify.
type VerifyRequest struct {
	// Subject is the loaded subject (exactly one — multi-platform subjects
	// are verified one at a time by the caller).
	Subject *Subject
	// Predicate is the subject's provenance predicate.
	Predicate *Predicate
	// Mode selects the comparison strategy: "digest" (default) or
	// "artifact" (descriptor tree walk).
	Mode string
	// Materials resolver, same semantics as BuildRequest.Materials.
	Materials *MaterialsResolver
	// Network controls the replayed build's RUN-network mode.
	Network string
	// Secrets / SSH mirror the BuildRequest shape for secret pass-through.
	Secrets buildflags.Secrets
	SSH     []*buildflags.SSH
	// Output is an optional type=local --output spec for the VSA and diff
	// report.
	Output *buildflags.ExportEntry
	// Progress is the progress display mode for the replayed build.
	Progress progressui.DisplayMode
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
// On a mismatch the returned error is a typed CompareMismatchError wrapping
// the diff report; callers should not attempt to interpret Matched=false with
// nil error.
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
	default:
		return nil, errors.Errorf("unknown --compare mode %q", mode)
	}
	if req.Output != nil && req.Output.Type != "local" {
		return nil, errors.Errorf("unsupported verify --output type %q (want local)", req.Output.Type)
	}

	// Attestation-file subjects have no produced artifact to verify against.
	if req.Subject.IsAttestationFile() {
		return nil, ErrUnsupportedSubject("verify requires an image or oci-layout subject")
	}
	if err := checkReplayable(req.Predicate, BuildModeMaterials, req.Secrets, req.SSH); err != nil {
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

// verifyReplay replays the subject into an OCI layout directory.
func verifyReplay(ctx context.Context, dockerCli command.Cli, builderName string, req *VerifyRequest, layoutDir string) error {
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
		Progress:    req.Progress,
	}

	return Build(ctx, dockerCli, builderName, breq)
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

// buildVSA returns an in-toto Statement containing a SLSA VSA predicate as a
// single-line JSON document.
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
					"digest": digestToDigestSet(req.Subject.AttestationManifest().Digest),
				},
			},
			// Buildx-specific fields that have no VSA equivalent.
			"buildx": map[string]any{
				"replayMode":    string(BuildModeMaterials),
				"compareMode":   mode,
				"replayDigest":  replayDesc.Digest.String(),
				"subjectDigest": req.Subject.Descriptor.Digest.String(),
			},
		},
	}
	dt, err := json.Marshal(statement)
	return dt, errors.WithStack(err)
}

// digestToDigestSet turns an OCI digest into the in-toto DigestSet shape.
func digestToDigestSet(d digest.Digest) map[string]string {
	if d == "" {
		return map[string]string{}
	}
	return map[string]string{d.Algorithm().String(): d.Encoded()}
}

// writeVerifyOutput writes the VSA and diff report to a type=local
// destination directory.
func writeVerifyOutput(req *VerifyRequest, result *VerifyResult, vsa []byte) error {
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
}
