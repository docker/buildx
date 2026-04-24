package replay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli/cli/command"
	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/attestation"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

const (
	dockerImagePrefix = "docker-image://"
	ociLayoutPrefix   = "oci-layout://"
)

// subjectKind classifies how a Subject was produced; downstream code only
// reads the exported fields, but the kind guides error messages.
type subjectKind int

const (
	// subjectKindImage covers both remote image refs and OCI-layout
	// inputs — both resolve to a (descriptor, content.Provider) tuple
	// with identical semantics.
	subjectKindImage subjectKind = iota
	subjectKindAttestationFile
)

// Subject is one replayable unit: a single manifest-level descriptor plus a
// content.Provider that serves that descriptor, its referrers, and the
// predicate blob.
//
// For image and oci-layout inputs, Descriptor is the produced artifact's
// manifest descriptor. For an attestation-file input, Descriptor points at
// the predicate blob in an in-memory content.Provider and there is no
// produced artifact.
type Subject struct {
	Descriptor ocispecs.Descriptor
	Provider   content.Provider

	// inputRef is the user-visible string for error messages.
	inputRef string
	kind     subjectKind
	// attestManifest is the in-toto attestation manifest descriptor that
	// referrers this subject (image / oci-layout kinds). Empty for
	// attestation-file subjects, whose predicate blob is directly at
	// Descriptor.
	attestManifest ocispecs.Descriptor
	// predicateType caches the predicate type URI for attestation-file
	// subjects so Predicate() can reject non-SLSA-v1 without re-reading.
	predicateType string
}

// IsAttestationFile reports whether this subject was loaded from a local
// attestation file (no produced artifact is available).
func (s *Subject) IsAttestationFile() bool { return s != nil && s.kind == subjectKindAttestationFile }

// InputRef returns the user-supplied input string that produced this
// subject. Used for diagnostics.
func (s *Subject) InputRef() string {
	if s == nil {
		return ""
	}
	return s.inputRef
}

// AttestationManifest returns the attestation manifest descriptor associated
// with this subject, or the zero descriptor if none was found (or the subject
// was loaded from a local attestation file).
func (s *Subject) AttestationManifest() ocispecs.Descriptor {
	if s == nil {
		return ocispecs.Descriptor{}
	}
	return s.attestManifest
}

// LoadSubjects parses a user-supplied input and returns one Subject per
// manifest to replay. An image index expands into N subjects (one per child
// manifest); a single image manifest or attestation file returns 1.
//
// Input forms:
//   - docker-image://<ref>       — explicit remote reference.
//   - oci-layout://<path>[:<tag>] — explicit OCI layout directory.
//   - <path-to-file>             — local attestation file (in-toto / DSSE).
//   - <path-to-directory>        — treated as an OCI layout.
//   - <bare ref>                 — valid image reference (docker-image).
func LoadSubjects(ctx context.Context, dockerCli command.Cli, builderName, input string) ([]*Subject, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, errors.New("empty subject input")
	}

	// Already-prefixed inputs go straight through.
	if strings.HasPrefix(trimmed, dockerImagePrefix) || strings.HasPrefix(trimmed, ociLayoutPrefix) {
		return loadImageSubjects(ctx, dockerCli, builderName, trimmed)
	}

	// A local filesystem path: regular file = attestation, directory =
	// OCI layout.
	if fi, err := os.Stat(trimmed); err == nil {
		if fi.IsDir() {
			return loadImageSubjects(ctx, dockerCli, builderName, ociLayoutPrefix+trimmed)
		}
		return loadAttestationFileSubject(trimmed)
	}

	// Fall through: treat as a remote image reference. Validation happens
	// inside loadImageSubjects.
	return loadImageSubjects(ctx, dockerCli, builderName, trimmed)
}

// loadAttestationFileSubject reads a local attestation file (in-toto
// Statement JSON, DSSE envelope, or an intoto.jsonl line-per-envelope file)
// and synthesizes a Subject whose Descriptor points at the predicate blob
// inside an in-memory content.Provider.
func loadAttestationFileSubject(path string) ([]*Subject, error) {
	dt, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	dt = bytes.TrimSpace(dt)
	if len(dt) == 0 {
		return nil, errors.Errorf("attestation file %s is empty", path)
	}

	// Heuristic: .intoto.jsonl is line-delimited JSON Statements. Pick the
	// first line that carries a provenance predicateType.
	if strings.HasSuffix(path, ".intoto.jsonl") {
		for line := range bytes.SplitSeq(dt, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			s, err := subjectFromAttestationBytes(line, path)
			if err == nil {
				return []*Subject{s}, nil
			}
		}
		return nil, errors.Errorf("no SLSA provenance statement found in %s", path)
	}

	s, err := subjectFromAttestationBytes(dt, path)
	if err != nil {
		return nil, err
	}
	return []*Subject{s}, nil
}

// subjectFromAttestationBytes parses a single in-toto Statement (or DSSE
// envelope around one) and returns a Subject whose Descriptor addresses the
// predicate bytes inside an in-memory content.Provider. Signed DSSE
// envelopes or Sigstore bundles are rejected with
// SignatureVerificationRequiredError — replay never silently accepts a
// signed attestation without a trust anchor. Full sigstore/cosign
// verification is tracked as a follow-up.
func subjectFromAttestationBytes(dt []byte, inputRef string) (*Subject, error) {
	// Sigstore bundle detection: a bundle carries mediaType
	// "application/vnd.dev.sigstore.bundle.v0.3+json" (or a v0.X variant)
	// and a non-empty verificationMaterial / dsseEnvelope pair. Detect
	// conservatively before the DSSE probe.
	var bundleProbe struct {
		MediaType            string          `json:"mediaType"`
		VerificationMaterial json.RawMessage `json:"verificationMaterial,omitempty"`
		DSSEEnvelope         json.RawMessage `json:"dsseEnvelope,omitempty"`
		MessageSignature     json.RawMessage `json:"messageSignature,omitempty"`
	}
	if err := json.Unmarshal(dt, &bundleProbe); err == nil {
		if strings.HasPrefix(bundleProbe.MediaType, "application/vnd.dev.sigstore.bundle.") ||
			(len(bundleProbe.VerificationMaterial) > 0 && (len(bundleProbe.DSSEEnvelope) > 0 || len(bundleProbe.MessageSignature) > 0)) {
			return nil, ErrSignatureVerificationRequired(inputRef, "sigstore-bundle")
		}
	}

	// DSSE envelopes have a "payload" field with base64 content plus an
	// optional "signatures" array. Detect by shape rather than media type
	// (files carry no MIME).
	var env struct {
		Payload     string `json:"payload"`
		PayloadType string `json:"payloadType"`
		Signatures  []struct {
			Sig   string `json:"sig"`
			KeyID string `json:"keyid,omitempty"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(dt, &env); err == nil && env.Payload != "" {
		// A signed DSSE envelope carries at least one non-empty signature.
		// Reject it — replay has no trust anchor. Full sigstore/cosign
		// signature verification is not implemented.
		for _, sig := range env.Signatures {
			if sig.Sig != "" {
				return nil, ErrSignatureVerificationRequired(inputRef, "dsse")
			}
		}
		decoded, err := decodeDSSEPayload(env.Payload)
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode DSSE payload")
		}
		dt = decoded
	}

	var stmt struct {
		PredicateType string          `json:"predicateType"`
		Predicate     json.RawMessage `json:"predicate"`
	}
	if err := json.Unmarshal(dt, &stmt); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal in-toto statement")
	}
	if stmt.PredicateType == "" {
		return nil, errors.Errorf("attestation file %s has no predicateType", inputRef)
	}

	predBytes := []byte(stmt.Predicate)
	dgst := digest.FromBytes(predBytes)
	buf := contentutil.NewBuffer()
	if err := content.WriteBlob(context.Background(), buf, dgst.String(), bytes.NewReader(predBytes), ocispecs.Descriptor{Digest: dgst, Size: int64(len(predBytes))}); err != nil {
		return nil, errors.WithStack(err)
	}

	desc := ocispecs.Descriptor{
		MediaType: "application/json",
		Digest:    dgst,
		Size:      int64(len(predBytes)),
	}
	return &Subject{
		Descriptor:    desc,
		Provider:      buf,
		inputRef:      inputRef,
		kind:          subjectKindAttestationFile,
		predicateType: stmt.PredicateType,
	}, nil
}

// decodeDSSEPayload base64-decodes the DSSE payload. Tries the standard
// Base64 alphabet first, then URL alphabet (some implementations use the URL
// variant for JSON-in-JSON safety).
func decodeDSSEPayload(payload string) ([]byte, error) {
	if dt, err := base64.StdEncoding.DecodeString(payload); err == nil {
		return dt, nil
	}
	if dt, err := base64.URLEncoding.DecodeString(payload); err == nil {
		return dt, nil
	}
	return nil, errors.New("payload is neither std nor url base64")
}

// loadImageSubjects resolves a remote ref or an oci-layout://<path>[:<tag>]
// input into one Subject per child manifest, fanning out only if the
// resolved descriptor is itself an index. Both shapes use the same
// util/imagetools.Resolver path as `imagetools inspect`; the resolver
// internally dispatches on the location shape (see
// util/imagetools/inspect.go:80).
func loadImageSubjects(ctx context.Context, dockerCli command.Cli, builderName, input string) ([]*Subject, error) {
	var (
		resolver *imagetools.Resolver
	)

	if imagetools.IsOCILayout(input) {
		// oci-layout:// does not need a builder / auth provider. Drive
		// Resolve + Fetcher through a default resolver backed purely by
		// the local layout store.
		resolver = imagetools.New(imagetools.Opt{})
	} else {
		trimmed := strings.TrimPrefix(input, dockerImagePrefix)
		if _, err := reference.ParseNormalizedNamed(trimmed); err != nil {
			return nil, errors.Wrapf(err, "invalid image reference %q", trimmed)
		}
		input = trimmed

		if dockerCli == nil {
			return nil, errors.New("docker CLI is required to resolve remote image subjects")
		}
		b, err := builder.New(dockerCli, builder.WithName(builderName))
		if err != nil {
			return nil, err
		}
		imageOpt, err := b.ImageOpt()
		if err != nil {
			return nil, err
		}
		resolver = imagetools.New(imageOpt)
	}

	_, desc, err := resolver.Resolve(ctx, input)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to resolve %s", input)
	}

	fetcher, err := resolver.Fetcher(ctx, input)
	if err != nil {
		return nil, err
	}
	provider := contentutil.FromFetcher(fetcher)

	return fanOutSubjects(ctx, provider, desc, input)
}

// fanOutSubjects walks an OCI index (if the root descriptor is an index) and
// returns one Subject per non-attestation child manifest. When the root is
// itself a manifest, a single subject is returned.
func fanOutSubjects(ctx context.Context, provider content.Provider, root ocispecs.Descriptor, inputRef string) ([]*Subject, error) {
	switch root.MediaType {
	case ocispecs.MediaTypeImageIndex, images.MediaTypeDockerSchema2ManifestList:
		dt, err := content.ReadBlob(ctx, provider, root)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		var idx ocispecs.Index
		if err := json.Unmarshal(dt, &idx); err != nil {
			return nil, errors.WithStack(err)
		}

		// Buildx snapshot layouts carry their own artifactType and reach
		// the attestation chain via the index's `subject`, not through a
		// sibling attestation manifest. Loading them as subjects requires
		// a different walk that is not yet implemented.
		if idx.ArtifactType == ArtifactTypeSnapshot {
			return nil, errors.Errorf("%s is a buildx snapshot layout; loading snapshots as subjects is not yet supported", inputRef)
		}

		// Partition entries: attestation manifests (via the Docker ref
		// annotation) vs. real image manifests. The attestation manifest
		// for a given subject carries `vnd.docker.reference.digest` /
		// `com.docker.reference.digest` pointing at the subject's digest.
		attestFor := map[digest.Digest]ocispecs.Descriptor{}
		var imageManifests []ocispecs.Descriptor
		for _, m := range idx.Manifests {
			if ref := attestationReferenceDigest(m); ref != "" {
				if d, err := digest.Parse(ref); err == nil {
					attestFor[d] = m
					continue
				}
			}
			imageManifests = append(imageManifests, m)
		}

		out := make([]*Subject, 0, len(imageManifests))
		for _, m := range imageManifests {
			s := &Subject{
				Descriptor: m,
				Provider:   provider,
				inputRef:   inputRef,
				kind:       subjectKindImage,
			}
			if att, ok := attestFor[m.Digest]; ok {
				s.attestManifest = att
			}
			out = append(out, s)
		}
		if len(out) == 0 {
			return nil, errors.Errorf("index %s has no image manifests", root.Digest)
		}
		return out, nil
	case ocispecs.MediaTypeImageManifest, images.MediaTypeDockerSchema2Manifest:
		return []*Subject{{
			Descriptor: root,
			Provider:   provider,
			inputRef:   inputRef,
			kind:       subjectKindImage,
		}}, nil
	default:
		return nil, errors.Errorf("unsupported root media type %q", root.MediaType)
	}
}

// attestationReferenceDigest returns the subject digest recorded on an
// attestation manifest's descriptor annotations, or "" if not present.
// Keep in sync with util/imagetools/loader.go annotationReferences list.
func attestationReferenceDigest(d ocispecs.Descriptor) string {
	if d.Annotations == nil {
		return ""
	}
	for _, k := range []string{
		attestation.DockerAnnotationReferenceDigest,
		"vnd.docker.reference.digest",
	} {
		if v, ok := d.Annotations[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// Predicate locates and parses the SLSA v1 provenance predicate attached to
// Descriptor via Provider. Returns UnsupportedPredicateError on a non-v1
// predicateType and NoProvenanceError when none is found.
func (s *Subject) Predicate(ctx context.Context) (*Predicate, error) {
	if s == nil {
		return nil, errors.New("nil subject")
	}

	switch s.kind {
	case subjectKindAttestationFile:
		dt, err := content.ReadBlob(ctx, s.Provider, s.Descriptor)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		return decodeProvenancePredicate(dt, s.predicateType)

	case subjectKindImage:
		if s.attestManifest.Digest == "" {
			return nil, ErrNoProvenance(s.inputRef)
		}
		predDt, predType, err := imagetools.ReadProvenancePredicate(ctx, s.Provider, s.attestManifest)
		if err != nil {
			return nil, err
		}
		if predType == "" {
			return nil, ErrNoProvenance(s.inputRef)
		}
		return decodeProvenancePredicate(predDt, predType)
	}

	return nil, ErrUnsupportedSubject("unknown")
}

// decodeProvenancePredicate unmarshals a provenance predicate in its
// native form. SLSA v1 is used as-is; SLSA v0.2 is converted to v1 via
// provenancetypes.ProvenancePredicateSLSA02.ConvertToSLSA1 so the rest of
// the replay code only has to understand one shape.
func decodeProvenancePredicate(dt []byte, predType string) (*Predicate, error) {
	switch predType {
	case slsa1.PredicateSLSAProvenance:
		var pred Predicate
		if err := json.Unmarshal(dt, &pred); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal SLSA v1 predicate")
		}
		return &pred, nil
	case slsa02.PredicateSLSAProvenance:
		var old provenancetypes.ProvenancePredicateSLSA02
		if err := json.Unmarshal(dt, &old); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal SLSA v0.2 predicate")
		}
		converted := old.ConvertToSLSA1()
		pred := Predicate(*converted)
		return &pred, nil
	}
	return nil, ErrUnsupportedPredicate(predType)
}
