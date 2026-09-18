package replay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli/cli/command"
	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	slsa1 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/attestation"
	"github.com/moby/buildkit/util/contentutil"
	policyverifier "github.com/moby/policy-helpers"
	policyimage "github.com/moby/policy-helpers/image"
	policytypes "github.com/moby/policy-helpers/types"
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
	// rootDescriptor is the image index from which this manifest subject was
	// selected. Image signature verification binds the index, selected
	// platform, attestation manifest, and its signature referrer together.
	rootDescriptor ocispecs.Descriptor
	// signature is populated only after a Sigstore bundle has passed
	// certificate, transparency-log, timestamp, and payload verification.
	signature *SignatureVerification
}

// SignatureVerification describes a cryptographically verified Sigstore
// bundle. Identity is informational: replay accepts unsigned provenance too,
// so verification does not imply that a separate authorization policy has
// approved this signer.
type SignatureVerification struct {
	Verified               bool                 `json:"verified"`
	Type                   string               `json:"type"`
	Identity               string               `json:"identity"`
	CertificateIssuer      string               `json:"certificateIssuer,omitempty"`
	SubjectAlternativeName string               `json:"subjectAlternativeName,omitempty"`
	Issuer                 string               `json:"issuer,omitempty"`
	SourceRepositoryURI    string               `json:"sourceRepositoryURI,omitempty"`
	SourceRepositoryRef    string               `json:"sourceRepositoryRef,omitempty"`
	BuildSignerURI         string               `json:"buildSignerURI,omitempty"`
	RunnerEnvironment      string               `json:"runnerEnvironment,omitempty"`
	Timestamps             []SignatureTimestamp `json:"timestamps,omitempty"`
	TrustRootLastUpdated   *time.Time           `json:"trustRootLastUpdated,omitempty"`
	TrustRootWarning       string               `json:"trustRootWarning,omitempty"`
}

// SignatureTimestamp is one verified observer timestamp from a Sigstore
// bundle, typically a transparency-log or timestamp-authority observation.
type SignatureTimestamp struct {
	Type      string    `json:"type"`
	URI       string    `json:"uri,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// IsAttestationFile reports whether this subject was loaded from a local
// attestation file (no produced artifact is available).
func (s *Subject) IsAttestationFile() bool { return s != nil && s.kind == subjectKindAttestationFile }

// Signature returns verified signature metadata, or nil for unsigned
// provenance and images.
func (s *Subject) Signature() *SignatureVerification {
	if s == nil {
		return nil
	}
	return s.signature
}

// VerifySignatures verifies signatures attached to the selected image
// subjects' provenance attestations. One verifier provider is shared across
// all platforms so its trust-root state is initialized only once. Unsigned
// images remain valid replay inputs; a published but invalid signature fails
// closed. Standalone Sigstore bundles are already verified while loading.
func VerifySignatures(ctx context.Context, dockerCli command.Cli, subjects []*Subject) error {
	var candidates []*Subject
	for _, s := range subjects {
		if s != nil && s.kind == subjectKindImage && s.rootDescriptor.MediaType == ocispecs.MediaTypeImageIndex && s.attestManifest.Digest != "" {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if dockerCli == nil {
		return errors.New("docker CLI is required to verify image signatures")
	}
	getVerifier := policy.SignatureVerifier(confutil.NewConfig(dockerCli))
	return verifySubjectSignatures(ctx, candidates, func(ctx context.Context, provider policyimage.ReferrersProvider, root ocispecs.Descriptor, platform *ocispecs.Platform) (*policytypes.SignatureInfo, error) {
		verifier, err := getVerifier()
		if err != nil {
			return nil, err
		}
		return verifier.VerifyImage(ctx, provider, root, platform)
	})
}

type imageSignatureVerifier func(context.Context, policyimage.ReferrersProvider, ocispecs.Descriptor, *ocispecs.Platform) (*policytypes.SignatureInfo, error)

func verifySubjectSignatures(ctx context.Context, subjects []*Subject, verify imageSignatureVerifier) error {
	for _, s := range subjects {
		if err := s.verifyImageSignature(ctx, verify); err != nil {
			return err
		}
	}
	return nil
}

func (s *Subject) verifyImageSignature(ctx context.Context, verify imageSignatureVerifier) error {
	provider, ok := s.Provider.(policyimage.ReferrersProvider)
	if !ok {
		return errors.Errorf("image provider for %s does not support referrers", s.inputRef)
	}
	si, err := verify(ctx, provider, s.rootDescriptor, s.Descriptor.Platform)
	if err != nil {
		var noSignature *policyverifier.NoSigChainError
		if errors.As(err, &noSignature) {
			return nil
		}
		return errors.Wrapf(err, "verify image signature for %s", s.inputRef)
	}
	if si == nil {
		return errors.Errorf("signature verifier returned no verification result for %s", s.inputRef)
	}
	s.signature = signatureVerification(si)
	return nil
}

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
		return loadAttestationFileSubject(ctx, dockerCli, trimmed)
	}

	// Fall through: treat as a remote image reference. Validation happens
	// inside loadImageSubjects.
	return loadImageSubjects(ctx, dockerCli, builderName, trimmed)
}

// loadAttestationFileSubject reads a local attestation file (in-toto
// Statement JSON, DSSE envelope, or an intoto.jsonl line-per-envelope file)
// and synthesizes a Subject whose Descriptor points at the predicate blob
// inside an in-memory content.Provider.
func loadAttestationFileSubject(ctx context.Context, dockerCli command.Cli, path string) ([]*Subject, error) {
	dt, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	dt = bytes.TrimSpace(dt)
	if len(dt) == 0 {
		return nil, errors.Errorf("attestation file %s is empty", path)
	}

	var verify artifactBundleVerifier
	if dockerCli != nil {
		getVerifier := policy.SignatureVerifier(confutil.NewConfig(dockerCli))
		verify = func(ctx context.Context, dgst digest.Digest, bundle []byte) (*policytypes.SignatureInfo, error) {
			verifier, err := getVerifier()
			if err != nil {
				return nil, err
			}
			return verifier.VerifyArtifact(ctx, dgst, bundle)
		}
	}
	dt, signature, isBundle, err := verifySigstoreBundle(ctx, dt, path, verify)
	if err != nil {
		return nil, err
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
				s.signature = signature
				return []*Subject{s}, nil
			}
		}
		return nil, errors.Errorf("no SLSA provenance statement found in %s", path)
	}

	s, err := subjectFromAttestationBytes(dt, path)
	if err != nil {
		return nil, err
	}
	if isBundle {
		s.signature = signature
	}
	return []*Subject{s}, nil
}

type artifactBundleVerifier func(context.Context, digest.Digest, []byte) (*policytypes.SignatureInfo, error)

func verifySigstoreBundle(ctx context.Context, dt []byte, inputRef string, verify artifactBundleVerifier) ([]byte, *SignatureVerification, bool, error) {
	var bundle struct {
		MediaType            string          `json:"mediaType"`
		VerificationMaterial json.RawMessage `json:"verificationMaterial,omitempty"`
		DSSEEnvelope         struct {
			Payload string `json:"payload"`
		} `json:"dsseEnvelope"`
		MessageSignature json.RawMessage `json:"messageSignature,omitempty"`
	}
	if err := json.Unmarshal(dt, &bundle); err != nil {
		return dt, nil, false, nil
	}
	isBundle := strings.HasPrefix(bundle.MediaType, "application/vnd.dev.sigstore.bundle.") ||
		(len(bundle.VerificationMaterial) > 0 && (bundle.DSSEEnvelope.Payload != "" || len(bundle.MessageSignature) > 0))
	if !isBundle {
		return dt, nil, false, nil
	}
	if verify == nil {
		return nil, nil, true, ErrSignatureVerificationRequired(inputRef, "sigstore-bundle")
	}
	if bundle.DSSEEnvelope.Payload == "" {
		return nil, nil, true, errors.Errorf("sigstore bundle %s does not contain a DSSE provenance envelope", inputRef)
	}
	payload, err := decodeDSSEPayload(bundle.DSSEEnvelope.Payload)
	if err != nil {
		return nil, nil, true, errors.Wrap(err, "decode sigstore DSSE payload")
	}
	var stmt struct {
		Subject []struct {
			Digest map[string]string `json:"digest"`
		} `json:"subject"`
	}
	if err := json.Unmarshal(payload, &stmt); err != nil {
		return nil, nil, true, errors.Wrap(err, "parse sigstore in-toto statement")
	}
	var lastErr error
	for _, subject := range stmt.Subject {
		dgst := preferredDigest(subject.Digest)
		if dgst == "" {
			continue
		}
		if err := dgst.Validate(); err != nil {
			lastErr = errors.WithStack(err)
			continue
		}
		si, err := verify(ctx, dgst, dt)
		if err != nil {
			lastErr = err
			continue
		}
		if si == nil {
			lastErr = errors.New("signature verifier returned no verification result")
			continue
		}
		return payload, signatureVerification(si), true, nil
	}
	if lastErr != nil {
		return nil, nil, true, errors.Wrap(lastErr, "verify sigstore bundle")
	}
	return nil, nil, true, errors.Errorf("sigstore bundle %s has no verifiable subject digest", inputRef)
}

func signatureVerification(si *policytypes.SignatureInfo) *SignatureVerification {
	if si == nil {
		return nil
	}
	out := &SignatureVerification{
		Verified:             true,
		Type:                 si.SignatureType.String(),
		Identity:             si.Name(),
		TrustRootLastUpdated: si.TrustRootStatus.LastUpdated,
		TrustRootWarning:     si.TrustRootStatus.Error,
	}
	if si.Signer != nil {
		out.CertificateIssuer = si.Signer.CertificateIssuer
		out.SubjectAlternativeName = si.Signer.SubjectAlternativeName
		out.Issuer = si.Signer.Issuer
		out.SourceRepositoryURI = si.Signer.SourceRepositoryURI
		out.SourceRepositoryRef = si.Signer.SourceRepositoryRef
		out.BuildSignerURI = si.Signer.BuildSignerURI
		out.RunnerEnvironment = si.Signer.RunnerEnvironment
	}
	for _, ts := range si.Timestamps {
		out.Timestamps = append(out.Timestamps, SignatureTimestamp{
			Type:      ts.Type,
			URI:       ts.URI,
			Timestamp: ts.Timestamp,
		})
	}
	return out
}

// subjectFromAttestationBytes parses a single in-toto Statement (or DSSE
// envelope around one) and returns a Subject whose Descriptor addresses the
// predicate bytes inside an in-memory content.Provider. Signed DSSE
// envelopes or Sigstore bundles are rejected with
// SignatureVerificationRequiredError — replay never silently accepts a
// signed attestation without a trust anchor. Standalone Sigstore bundles are
// verified and unwrapped by loadAttestationFileSubject before reaching here.
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
		// Reject a bare signed envelope: unlike a Sigstore bundle, it carries
		// no verification material from which replay can establish trust.
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

	if strings.HasPrefix(input, ociLayoutPrefix) {
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
	loc, err := imagetools.ParseLocation(input)
	if err != nil {
		return nil, err
	}
	provider := &imageSubjectProvider{
		Provider: contentutil.FromFetcher(fetcher),
		resolver: resolver,
		location: loc,
	}

	return fanOutSubjects(ctx, provider, desc, input)
}

// imageSubjectProvider combines the ordinary content provider used to read
// manifests and blobs with imagetools' registry/OCI-layout referrer lookup.
// policy-helpers can therefore verify the exact signature chain without a
// second resolver implementation in replay.
type imageSubjectProvider struct {
	content.Provider
	resolver *imagetools.Resolver
	location *imagetools.Location
}

func (p *imageSubjectProvider) FetchReferrers(ctx context.Context, dgst digest.Digest, opts ...remotes.FetchReferrersOpt) ([]ocispecs.Descriptor, error) {
	return p.resolver.FetchReferrers(ctx, p.location, dgst, opts...)
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
					if _, ok := attestFor[d]; !ok {
						attestFor[d] = m
					}
					continue
				}
			}
			imageManifests = append(imageManifests, m)
		}

		out := make([]*Subject, 0, len(imageManifests))
		for _, m := range imageManifests {
			s := &Subject{
				Descriptor:     m,
				Provider:       provider,
				inputRef:       inputRef,
				kind:           subjectKindImage,
				rootDescriptor: root,
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
			Descriptor:     root,
			Provider:       provider,
			inputRef:       inputRef,
			kind:           subjectKindImage,
			rootDescriptor: root,
		}}, nil
	default:
		return nil, errors.Errorf("unsupported root media type %q", root.MediaType)
	}
}

// attestationReferenceDigest returns the subject digest recorded on a BuildKit
// attestation manifest, or "" for an unrelated descriptor. Selection exactly
// matches policy-helpers' signature-chain resolver: correct reference type,
// correct digest, and first matching descriptor wins in index order.
func attestationReferenceDigest(d ocispecs.Descriptor) string {
	if d.Annotations == nil || d.Annotations[attestation.DockerAnnotationReferenceType] != attestation.DockerAnnotationReferenceTypeDefault {
		return ""
	}
	return d.Annotations[attestation.DockerAnnotationReferenceDigest]
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
