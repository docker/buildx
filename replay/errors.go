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
	if len(e.LocalSources) == 0 {
		return "build used local context that cannot be replayed"
	}
	return fmt.Sprintf("build used local context that cannot be replayed: %s", strings.Join(e.LocalSources, ", "))
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
	return fmt.Sprintf("missing required secrets: %s", strings.Join(e.IDs, ", "))
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
	return fmt.Sprintf("missing required ssh entries: %s", strings.Join(e.IDs, ", "))
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
}

func (e *NoProvenanceError) Error() string {
	if e.Subject == "" {
		return "no SLSA provenance attestation found"
	}
	return fmt.Sprintf("no SLSA provenance attestation found for %s", e.Subject)
}

// ErrNoProvenance constructs a NoProvenanceError.
func ErrNoProvenance(subject string) error {
	return errors.WithStack(&NoProvenanceError{Subject: subject})
}

// UnsupportedPredicateError signals that the attached provenance predicate is
// not SLSA v1.
type UnsupportedPredicateError struct {
	PredicateType string
}

func (e *UnsupportedPredicateError) Error() string {
	return fmt.Sprintf("unsupported provenance predicate type %q; replay requires SLSA v1", e.PredicateType)
}

// ErrUnsupportedPredicate constructs an UnsupportedPredicateError.
func ErrUnsupportedPredicate(predicateType string) error {
	return errors.WithStack(&UnsupportedPredicateError{PredicateType: predicateType})
}

// BuildKitCapMissingError is returned when the target BuildKit daemon lacks
// a capability required by replay (notably CapSourcePolicySession).
type BuildKitCapMissingError struct {
	Capability string
}

func (e *BuildKitCapMissingError) Error() string {
	return fmt.Sprintf("BuildKit daemon missing required capability %q; please upgrade", e.Capability)
}

// ErrBuildKitCapMissing constructs a BuildKitCapMissingError.
func ErrBuildKitCapMissing(capability string) error {
	return errors.WithStack(&BuildKitCapMissingError{Capability: capability})
}

// SignatureVerificationRequiredError is returned when a signed DSSE envelope
// or Sigstore bundle is encountered but no trust anchor is available. Replay
// never silently accepts a signed attestation; full sigstore/cosign
// verification is not yet implemented.
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
	return fmt.Sprintf("%s for %s carries signatures but signature verification is not yet implemented; refusing to accept unverified signed attestation", env, src)
}

// ErrSignatureVerificationRequired constructs a
// SignatureVerificationRequiredError.
func ErrSignatureVerificationRequired(source, envelope string) error {
	return errors.WithStack(&SignatureVerificationRequiredError{Source: source, Envelope: envelope})
}

// MissingRequiredFlagError is returned when a replay subcommand is invoked
// without a flag that is required (e.g. `replay snapshot` without `--output`).
type MissingRequiredFlagError struct {
	Flag string
}

func (e *MissingRequiredFlagError) Error() string {
	if e.Flag == "" {
		return "missing required flag"
	}
	return fmt.Sprintf("missing required flag %s", e.Flag)
}

// ErrMissingRequiredFlag constructs a MissingRequiredFlagError.
func ErrMissingRequiredFlag(flag string) error {
	return errors.WithStack(&MissingRequiredFlagError{Flag: flag})
}
