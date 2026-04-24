package replay

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

// TestErrorTypes asserts that each replay constructor returns the typed error
// expected by errors.As-based consumers. Exit-code mapping lives with the CLI
// glue (commands/replay/errors.go) and is tested there.
func TestErrorTypes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want any
	}{
		{"local context", ErrUnreplayableLocalContext([]string{"default"}), &UnreplayableLocalContextError{}},
		{"missing secret", ErrMissingSecret([]string{"mysecret"}), &MissingSecretError{}},
		{"extra secret", ErrExtraSecret([]string{"mysecret"}), &ExtraSecretError{}},
		{"missing ssh", ErrMissingSSH([]string{"default"}), &MissingSSHError{}},
		{"extra ssh", ErrExtraSSH([]string{"default"}), &ExtraSSHError{}},
		{"material not found", ErrMaterialNotFound("docker-image://foo", "sha256:aa"), &MaterialNotFoundError{}},
		{"compare mismatch", ErrCompareMismatch("digest differs", nil), &CompareMismatchError{}},
		{"not implemented", ErrNotImplemented("llb replay mode"), &NotImplementedError{}},
		{"unsupported subject", ErrUnsupportedSubject("attestation-file"), &UnsupportedSubjectError{}},
		{"no provenance", ErrNoProvenance("foo:latest"), &NoProvenanceError{}},
		{"unsupported predicate", ErrUnsupportedPredicate("https://slsa.dev/provenance/v0.2"), &UnsupportedPredicateError{}},
		{"buildkit cap missing", ErrBuildKitCapMissing("CapSourcePolicySession"), &BuildKitCapMissingError{}},
		{"signature verification required", ErrSignatureVerificationRequired("./att.json", "dsse"), &SignatureVerificationRequiredError{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)
			switch want := tt.want.(type) {
			case *UnreplayableLocalContextError:
				require.ErrorAs(t, tt.err, &want)
			case *MissingSecretError:
				require.ErrorAs(t, tt.err, &want)
			case *ExtraSecretError:
				require.ErrorAs(t, tt.err, &want)
			case *MissingSSHError:
				require.ErrorAs(t, tt.err, &want)
			case *ExtraSSHError:
				require.ErrorAs(t, tt.err, &want)
			case *MaterialNotFoundError:
				require.ErrorAs(t, tt.err, &want)
			case *CompareMismatchError:
				require.ErrorAs(t, tt.err, &want)
			case *NotImplementedError:
				require.ErrorAs(t, tt.err, &want)
			case *UnsupportedSubjectError:
				require.ErrorAs(t, tt.err, &want)
			case *NoProvenanceError:
				require.ErrorAs(t, tt.err, &want)
			case *UnsupportedPredicateError:
				require.ErrorAs(t, tt.err, &want)
			case *BuildKitCapMissingError:
				require.ErrorAs(t, tt.err, &want)
			case *SignatureVerificationRequiredError:
				require.ErrorAs(t, tt.err, &want)
			default:
				t.Fatalf("unexpected want type %T", tt.want)
			}
		})
	}
}

// TestErrorsAsWrapped asserts that pkg/errors stack-wrapped and fmt-wrapped
// replay errors remain errors.As-matchable.
func TestErrorsAsWrapped(t *testing.T) {
	e := ErrMaterialNotFound("foo", "sha256:a")
	wrapped := errors.Wrap(e, "resolver failed")
	var mnf *MaterialNotFoundError
	require.ErrorAs(t, wrapped, &mnf)
	require.Equal(t, "foo", mnf.URI)
}

// TestSignatureVerificationRequiredMessage asserts the error message carries
// both the source and envelope kind so users know what was rejected.
func TestSignatureVerificationRequiredMessage(t *testing.T) {
	e := ErrSignatureVerificationRequired("/tmp/att.json", "dsse")
	require.Contains(t, e.Error(), "/tmp/att.json")
	require.Contains(t, e.Error(), "dsse")
}
