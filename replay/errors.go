package replay

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

// UnreplayableLocalContextError signals that the original build used a local
// filesystem context which replay cannot reproduce.
type UnreplayableLocalContextError struct {
	LocalSources []string
}

func (e *UnreplayableLocalContextError) Error() string {
	const hint = "only builds from a Git repository or HTTP(S) URL context can be replayed"
	if len(e.LocalSources) == 0 {
		return "image was built from local inputs that replay cannot fetch; " + hint
	}
	return fmt.Sprintf("image was built from local inputs that replay cannot fetch (%s); %s", strings.Join(e.LocalSources, ", "), hint)
}

// ErrUnreplayableLocalContext constructs an UnreplayableLocalContextError.
func ErrUnreplayableLocalContext(sources []string) error {
	sorted := append([]string(nil), sources...)
	sort.Strings(sorted)
	return errors.WithStack(&UnreplayableLocalContextError{LocalSources: sorted})
}

// MissingSecretError is returned when provenance declares required secrets
// that the user did not provide.
type MissingSecretError struct {
	IDs []string
}

func (e *MissingSecretError) Error() string {
	return fmt.Sprintf("missing required secrets: %s (pass them with --secret id=<id>,src=<path>)", strings.Join(e.IDs, ", "))
}

// ErrMissingSecret constructs a MissingSecretError.
func ErrMissingSecret(ids []string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return errors.WithStack(&MissingSecretError{IDs: sorted})
}

// ExtraSecretError is returned when the user supplies secrets that the
// provenance does not declare.
type ExtraSecretError struct {
	IDs []string
}

func (e *ExtraSecretError) Error() string {
	return fmt.Sprintf("extra secrets not declared in provenance: %s", strings.Join(e.IDs, ", "))
}

// ErrExtraSecret constructs an ExtraSecretError.
func ErrExtraSecret(ids []string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return errors.WithStack(&ExtraSecretError{IDs: sorted})
}

// MissingSSHError is returned when provenance declares required SSH agents
// that the user did not provide.
type MissingSSHError struct {
	IDs []string
}

func (e *MissingSSHError) Error() string {
	return fmt.Sprintf("missing required ssh entries: %s (pass them with --ssh <id>=<socket|key>)", strings.Join(e.IDs, ", "))
}

// ErrMissingSSH constructs a MissingSSHError.
func ErrMissingSSH(ids []string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return errors.WithStack(&MissingSSHError{IDs: sorted})
}

// ExtraSSHError is returned when the user supplies SSH agents that the
// provenance does not declare.
type ExtraSSHError struct {
	IDs []string
}

func (e *ExtraSSHError) Error() string {
	return fmt.Sprintf("extra ssh entries not declared in provenance: %s", strings.Join(e.IDs, ", "))
}

// ErrExtraSSH constructs an ExtraSSHError.
func ErrExtraSSH(ids []string) error {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	return errors.WithStack(&ExtraSSHError{IDs: sorted})
}

// MaterialNotFoundError indicates a provenance material that the resolver
// could not locate in any configured store.
type MaterialNotFoundError struct {
	URI    string
	Digest string
}

func (e *MaterialNotFoundError) Error() string {
	return fmt.Sprintf("material not found: uri=%q digest=%q", e.URI, e.Digest)
}

// ErrMaterialNotFound constructs a MaterialNotFoundError.
func ErrMaterialNotFound(uri, dgst string) error {
	return errors.WithStack(&MaterialNotFoundError{URI: uri, Digest: dgst})
}

// CompareMismatchError is returned by `replay verify` when the replayed
// artifact does not match the subject. The wrapped Report may be nil when no
// structured diff is available (digest comparison).
type CompareMismatchError struct {
	// Report is typed as any so callers can surface either the basic compare
	// tree or a future richer report format without breaking the error type.
	Report any
	Reason string
}

func (e *CompareMismatchError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("replay mismatch: %s", e.Reason)
	}
	return "replay mismatch"
}

// ErrCompareMismatch constructs a CompareMismatchError.
func ErrCompareMismatch(reason string, report any) error {
	return errors.WithStack(&CompareMismatchError{Reason: reason, Report: report})
}

// NotImplementedError marks a feature that is not yet implemented.
type NotImplementedError struct {
	Feature string
}

func (e *NotImplementedError) Error() string {
	return fmt.Sprintf("not implemented: %s", e.Feature)
}

// ErrNotImplemented constructs a NotImplementedError.
func ErrNotImplemented(feature string) error {
	return errors.WithStack(&NotImplementedError{Feature: feature})
}

// UnsupportedSubjectError signals that the supplied subject kind is not
// compatible with the invoked subcommand.
type UnsupportedSubjectError struct {
	Kind string
}

func (e *UnsupportedSubjectError) Error() string {
	return fmt.Sprintf("unsupported subject: %s", e.Kind)
}

// ErrUnsupportedSubject constructs an UnsupportedSubjectError.
func ErrUnsupportedSubject(kind string) error {
	return errors.WithStack(&UnsupportedSubjectError{Kind: kind})
}

// NoProvenanceError is returned when no SLSA provenance attestation could be
// found for a subject.
type NoProvenanceError struct {
	Subject string
	// Manifest is set when the subject is an image manifest referenced
	// directly rather than through its image index.
	Manifest bool
}

func (e *NoProvenanceError) Error() string {
	hint := "build the image with --provenance=mode=max to make it replayable"
	if e.Manifest {
		hint = "if this is a platform manifest of a multi-platform image, use the image index reference instead; otherwise " + hint
	}
	if e.Subject == "" {
		return "no SLSA provenance attestation found; " + hint
	}
	return fmt.Sprintf("no SLSA provenance attestation found for %s; %s", e.Subject, hint)
}

// ErrNoProvenance constructs a NoProvenanceError.
func ErrNoProvenance(subject string) error {
	return errors.WithStack(&NoProvenanceError{Subject: subject})
}

// ErrNoProvenanceForManifest constructs a NoProvenanceError for an image
// manifest referenced directly.
func ErrNoProvenanceForManifest(subject string) error {
	return errors.WithStack(&NoProvenanceError{Subject: subject, Manifest: true})
}

// UnsupportedPredicateError signals that the attached predicate is not SLSA
// provenance.
type UnsupportedPredicateError struct {
	PredicateType string
}

func (e *UnsupportedPredicateError) Error() string {
	return fmt.Sprintf("unsupported predicate type %q; replay requires SLSA provenance (v0.2 or v1)", e.PredicateType)
}

// ErrUnsupportedPredicate constructs an UnsupportedPredicateError.
func ErrUnsupportedPredicate(predicateType string) error {
	return errors.WithStack(&UnsupportedPredicateError{PredicateType: predicateType})
}

// SignatureVerificationRequiredError is returned when a signed envelope
// cannot be verified from the available trust material. Replay never silently
// unwraps such an attestation.
type SignatureVerificationRequiredError struct {
	// Source is the user-visible input that carries the signed envelope
	// (file path for attestation-file inputs).
	Source string
	// Envelope describes the detected envelope shape ("dsse" or
	// "sigstore-bundle") so the user can tell what was rejected.
	Envelope string
}

func (e *SignatureVerificationRequiredError) Error() string {
	src := e.Source
	if src == "" {
		src = "attestation"
	}
	env := e.Envelope
	if env == "" {
		env = "signed envelope"
	}
	return fmt.Sprintf("%s for %s carries signatures but cannot be verified from the available trust material; refusing to accept unverified signed attestation", env, src)
}

// ErrSignatureVerificationRequired constructs a
// SignatureVerificationRequiredError.
func ErrSignatureVerificationRequired(source, envelope string) error {
	return errors.WithStack(&SignatureVerificationRequiredError{Source: source, Envelope: envelope})
}

// MinModeProvenanceError is returned when the provenance was recorded with
// mode=min, which omits the build arguments, secrets and SSH needed to
// reconstruct the build.
type MinModeProvenanceError struct{}

func (e *MinModeProvenanceError) Error() string {
	return "provenance was recorded with mode=min, which omits build arguments, secrets and SSH needed for replay; build the image with --provenance=mode=max to make it replayable"
}

// ErrMinModeProvenance constructs a MinModeProvenanceError.
func ErrMinModeProvenance() error {
	return errors.WithStack(&MinModeProvenanceError{})
}

// UnpinnedContextError is returned when the provenance does not record a
// digest for a Git subdirectory build context, so replay cannot pin it.
type UnpinnedContextError struct {
	URI string
}

func (e *UnpinnedContextError) Error() string {
	return fmt.Sprintf("cannot pin the Git build context %s: provenance from BuildKit before v0.25 does not record a digest for subdirectory contexts; use --replay-mode=frontend to replay without pinning sources", e.URI)
}

// ErrUnpinnedContext constructs an UnpinnedContextError.
func ErrUnpinnedContext(uri string) error {
	return errors.WithStack(&UnpinnedContextError{URI: uri})
}
