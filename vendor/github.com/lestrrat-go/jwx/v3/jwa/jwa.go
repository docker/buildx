//go:generate ../tools/cmd/genjwa.sh

// Package jwa defines the various algorithm described in https://tools.ietf.org/html/rfc7518
package jwa

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

const maxKeyAlgorithmErrorPreview = 64

// KeyAlgorithm is a workaround for jwk.Key being able to contain different
// types of algorithms in its `alg` field.
//
// Previously the storage for the `alg` field was represented as a string,
// but this caused some users to wonder why the field was not typed appropriately
// like other fields.
//
// Ideally we would like to keep track of Signature Algorithms and
// Key Encryption Algorithms separately, and force the APIs to
// type-check at compile time, but this allows users to pass a value from a
// jwk.Key directly
type KeyAlgorithm interface {
	String() string
	IsDeprecated() bool
}

var errInvalidKeyAlgorithm = errors.New(`invalid key algorithm`)

func ErrInvalidKeyAlgorithm() error {
	return errInvalidKeyAlgorithm
}

func formatInvalidKeyAlgorithmValue(v string) string {
	runes := []rune(v)
	if len(runes) <= maxKeyAlgorithmErrorPreview {
		return fmt.Sprintf("%q", v)
	}

	return fmt.Sprintf("%q", string(runes[:maxKeyAlgorithmErrorPreview])+`...`)
}

// algorithmKind tags entries in the shared algRegistry so the
// per-kind public Register/Lookup/Unregister/<Kind>s functions can
// dispatch through one map without losing the typed identity of each
// algorithm.
type algorithmKind uint8

const (
	algKindUnknown algorithmKind = iota
	algKindSignature
	algKindKeyEncryption
	algKindContentEncryption
)

func (k algorithmKind) String() string {
	switch k {
	case algKindSignature:
		return "SignatureAlgorithm"
	case algKindKeyEncryption:
		return "KeyEncryptionAlgorithm"
	case algKindContentEncryption:
		return "ContentEncryptionAlgorithm"
	default:
		return "unknown algorithm kind"
	}
}

type algRegistryEntry struct {
	kind    algorithmKind
	alg     KeyAlgorithm
	builtin bool
}

// algRegistry is the single shared namespace for the three
// KeyAlgorithm-implementing kinds. Independent per-kind maps would
// let an extension register the same name as both (say) a
// SignatureAlgorithm and a KeyEncryptionAlgorithm, after which
// KeyAlgorithmFrom("X") would resolve to whichever kind was tried
// first — silently flipping with import order. Funnelling all three
// through one map fixes that ambiguity at registration time.
var (
	muAlgRegistry sync.RWMutex
	algRegistry   = map[string]algRegistryEntry{}
)

// registerAlgorithm is the shared backend for the three public
// Register{Signature,KeyEncryption,ContentEncryption}Algorithm
// functions.
//
// Behavior:
//   - Re-registering the exact same value (same kind, same alg) is a
//     no-op.
//   - Cross-kind name reuse is a silent no-op: the first registration
//     wins and the second Register* call has no effect. v3's
//     pre-existing Register* signature returns no error and v3 does
//     not change observable error/panic behavior, so the cross-kind
//     case is silently skipped — KeyAlgorithmFrom now resolves
//     unambiguously to the first-registered kind. (v4 escalates this
//     to a returned error from Register*.)
//   - Built-in replacement is a silent no-op, preserving the
//     pre-unification per-kind v3 Register* behavior.
//   - Same-kind, non-builtin re-registration with a different value
//     silently overwrites. This preserves the pre-unification
//     behavior of the per-kind Register* functions.
func registerAlgorithm(kind algorithmKind, alg KeyAlgorithm) {
	name := alg.String()
	muAlgRegistry.Lock()
	defer muAlgRegistry.Unlock()
	if existing, ok := algRegistry[name]; ok {
		if existing.kind == kind && existing.alg == alg {
			return
		}
		if existing.kind != kind {
			return
		}
		if existing.builtin {
			return
		}
	}
	algRegistry[name] = algRegistryEntry{kind: kind, alg: alg}
}

// markBuiltin flips the builtin flag on an already-registered name.
// Called by the per-kind generated init() after the bulk Register*
// pass, preserving the existing two-phase init pattern.
func markBuiltin(name string) {
	muAlgRegistry.Lock()
	defer muAlgRegistry.Unlock()
	if entry, ok := algRegistry[name]; ok {
		entry.builtin = true
		algRegistry[name] = entry
	}
}

// unregisterAlgorithm is the shared backend for the three public
// Unregister*Algorithm functions. No-op for built-ins, no-op for a
// kind mismatch, no-op for unknown names — same surface contract as
// the pre-unification per-kind Unregister*.
func unregisterAlgorithm(kind algorithmKind, name string) {
	muAlgRegistry.Lock()
	defer muAlgRegistry.Unlock()
	if entry, ok := algRegistry[name]; ok && entry.kind == kind && !entry.builtin {
		delete(algRegistry, name)
	}
}

// lookupAlgorithm returns the registered KeyAlgorithm for name iff it
// is registered as the requested kind. Used by the per-kind
// generated Lookup* wrappers.
func lookupAlgorithm(kind algorithmKind, name string) (KeyAlgorithm, bool) {
	muAlgRegistry.RLock()
	defer muAlgRegistry.RUnlock()
	if entry, ok := algRegistry[name]; ok && entry.kind == kind {
		return entry.alg, true
	}
	return nil, false
}

// listAlgorithmsByKind returns every registered algorithm of the
// given kind, sorted by name. Used by the per-kind generated
// <Kind>s() functions.
func listAlgorithmsByKind(kind algorithmKind) []KeyAlgorithm {
	muAlgRegistry.RLock()
	defer muAlgRegistry.RUnlock()
	out := make([]KeyAlgorithm, 0, len(algRegistry))
	for _, entry := range algRegistry {
		if entry.kind == kind {
			out = append(out, entry.alg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// KeyAlgorithmFrom takes either a string, `jwa.SignatureAlgorithm`,
// `jwa.KeyEncryptionAlgorithm`, or `jwa.ContentEncryptionAlgorithm`,
// and returns a `jwa.KeyAlgorithm`.
//
// String inputs resolve through the shared algorithm registry: the
// returned KeyAlgorithm holds the concrete typed value (Signature,
// KeyEncryption, or ContentEncryption) for whichever kind owns the
// name. Cross-kind name reuse is structurally avoided — the first
// Register* wins and subsequent cross-kind registrations are silent
// no-ops — so KeyAlgorithmFrom no longer needs precedence rules.
//
// Typed inputs whose String() is empty (for example a zero-value
// `var sa jwa.SignatureAlgorithm`) are rejected with
// ErrInvalidKeyAlgorithm. Without this check the typed arms accepted
// names that would never resolve through any registry, surfacing as
// confusing failures far from the call site.
func KeyAlgorithmFrom(v any) (KeyAlgorithm, error) {
	switch v := v.(type) {
	case SignatureAlgorithm:
		if v.String() == "" {
			return nil, fmt.Errorf(`invalid key value: zero-value %T: %w`, v, errInvalidKeyAlgorithm)
		}
		return v, nil
	case KeyEncryptionAlgorithm:
		if v.String() == "" {
			return nil, fmt.Errorf(`invalid key value: zero-value %T: %w`, v, errInvalidKeyAlgorithm)
		}
		return v, nil
	case ContentEncryptionAlgorithm:
		if v.String() == "" {
			return nil, fmt.Errorf(`invalid key value: zero-value %T: %w`, v, errInvalidKeyAlgorithm)
		}
		return v, nil
	case string:
		muAlgRegistry.RLock()
		entry, ok := algRegistry[v]
		muAlgRegistry.RUnlock()
		if !ok {
			return nil, fmt.Errorf(`invalid key value: %s: %w`, formatInvalidKeyAlgorithmValue(v), errInvalidKeyAlgorithm)
		}
		return entry.alg, nil
	default:
		return nil, fmt.Errorf(`invalid key type: %T: %w`, v, errInvalidKeyAlgorithm)
	}
}
