package jws

import (
	"errors"
	"fmt"
)

// errCritPresent is returned by VerifyCompactFast when the protected
// header carries a "crit" list. The fast path cannot enforce RFC 7515
// §4.1.11 (it has no WithCritExtension allowlist), so it refuses rather
// than silently accepting. The sentinel is wrapped in verifyError at the
// return site so the resulting error matches BOTH errors.Is(err,
// jws.ErrCritPresent()) (the specific reason) AND errors.Is(err,
// jws.VerifyError()) (the general class), letting callers choose the
// classification granularity that fits their code path.
var errCritPresent = errors.New("VerifyCompactFast: protected header contains \"crit\"; use jws.Verify")

// ErrCritPresent returns the sentinel error returned by VerifyCompactFast
// when the protected header contains a "crit" list. The error returned
// from VerifyCompactFast also matches jws.VerifyError(), so callers that
// only branch on the general class still classify the refusal correctly.
func ErrCritPresent() error {
	return errCritPresent
}

// errB64Present is returned by VerifyCompactFast when the protected
// header carries a "b64" entry (typically b64=false per RFC 7797). The
// fast path assumes the default b64=true encoding for both the
// signing-input reconstruction and the post-verify payload decode; a
// b64=false message signed under non-conformant rules (b64 not declared
// in "crit") would otherwise verify cryptographically while returning
// a decoded payload that differs from the producer's intent. Refusing
// here defers such messages to jws.Verify, which has the
// WithDetachedPayload and WithCritExtension machinery to handle b64=false
// correctly. As with errCritPresent, the sentinel is wrapped in
// verifyError at the return site so the resulting error matches both
// errors.Is(err, jws.ErrB64Present()) and errors.Is(err, jws.VerifyError()).
var errB64Present = errors.New("VerifyCompactFast: protected header contains \"b64\"; use jws.Verify")

// ErrB64Present returns the sentinel error returned by VerifyCompactFast
// when the protected header contains a "b64" entry. The error returned
// from VerifyCompactFast also matches jws.VerifyError(), so callers that
// only branch on the general class still classify the refusal correctly.
func ErrB64Present() error {
	return errB64Present
}

// errUnclassifiableKey is the common sentinel for AlgorithmsForKey
// failures: the key shape cannot be matched to any registered key type
// for signing. Three different code paths land here — Import-failed,
// kty-not-registered, and shape-rejected (e.g. ecdh) — but they're all
// the same logical "we can't classify this key" outcome from the
// caller's perspective. Wrap-with-this lets callers branch on
// errors.Is(err, jws.ErrUnclassifiableKey()) instead of pattern-matching
// the three error-message shapes the function previously emitted.
var errUnclassifiableKey = errors.New("jws: key cannot be classified for signing")

// ErrUnclassifiableKey returns the sentinel that jws.AlgorithmsForKey
// (and indirectly jws.Sign / jws.Verify when option-time validation
// fails) wraps when the supplied key cannot be matched to a registered
// key type. Branching on this sentinel is the right way to ask "is this
// a 'we can't tell what this key is' failure?" — the wrapping error
// also carries the concrete %T or %q diagnostic in its message, so the
// human-readable error stays specific.
func ErrUnclassifiableKey() error {
	return errUnclassifiableKey
}

type signError struct {
	error
}

const (
	prefixJwsSign    = `jws.Sign`
	prefixJwsCompact = `jws.Compact`
)

var errDefaultSignError = makeSignError(prefixJwsSign, `unknown error`)

// SignError returns an error that can be passed to `errors.Is` to check if the error is a sign error.
func SignError() error {
	return errDefaultSignError
}

func (e signError) Unwrap() error {
	return e.error
}

func (signError) Is(err error) bool {
	_, ok := err.(signError)
	return ok
}

func makeSignError(prefix string, f string, args ...any) error {
	return signError{fmt.Errorf(prefix+`: `+f, args...)}
}

// This error is returned when jws.Verify fails, but note that there's another type of
// message that can be returned by jws.Verify, which is `errVerification`.
type verifyError struct {
	error
}

var errDefaultVerifyError = makeVerifyError(`unknown error`)

// VerifyError returns an error that can be passed to `errors.Is` to check if the error is a verify error.
func VerifyError() error {
	return errDefaultVerifyError
}

func (e verifyError) Unwrap() error {
	return e.error
}

func (verifyError) Is(err error) bool {
	_, ok := err.(verifyError)
	return ok
}

func makeVerifyError(f string, args ...any) error {
	return verifyError{fmt.Errorf(`jws.Verify: `+f, args...)}
}

// verificationError is returned when the actual _verification_ of the key/payload fails.
type verificationError struct {
	error
}

var errDefaultVerificationError = verificationError{fmt.Errorf(`unknown verification error`)}

// VerificationError returns an error that can be passed to `errors.Is` to check if the error is a verification error.
func VerificationError() error {
	return errDefaultVerificationError
}

func (e verificationError) Unwrap() error {
	return e.error
}

func (verificationError) Is(err error) bool {
	_, ok := err.(verificationError)
	return ok
}

type parseError struct {
	error
}

var errDefaultParseError = makeParseError(`jws.Parse`, `unknown error`)

// ParseError returns an error that can be passed to `errors.Is` to check if the error is a parse error.
func ParseError() error {
	return errDefaultParseError
}

func (e parseError) Unwrap() error {
	return e.error
}

func (parseError) Is(err error) bool {
	_, ok := err.(parseError)
	return ok
}

func makeParseError(prefix string, f string, args ...any) error {
	return parseError{fmt.Errorf(prefix+": "+f, args...)}
}
