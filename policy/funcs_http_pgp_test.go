package policy

import (
	"bytes"
	"crypto"
	"encoding/hex"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/moby/buildkit/util/pgpsign"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	digest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestBuiltinVerifyHTTPPGPSignatureImpl(t *testing.T) {
	const (
		sigPath = "sig.asc"
		keyPath = "pubkey.asc"
	)
	payload := []byte("buildx-http-payload")
	sigData, pubKeyData, checksumDigest, suffix := createDetachedPGPFixture(t, payload)

	newPolicy := func(sig []byte, pub []byte) *Policy {
		return NewPolicy(Opt{
			FS: func() (fs.StatFS, func() error, error) {
				return fstest.MapFS{
					sigPath: &fstest.MapFile{Data: sig},
					keyPath: &fstest.MapFile{Data: pub},
				}, func() error { return nil }, nil
			},
		})
	}

	t.Run("success", func(t *testing.T) {
		st := &state{
			Input: Input{
				HTTP: &HTTP{
					checksumResponseForSignature: &httpChecksumResponseForSignature{
						Digest: checksumDigest.String(),
						Suffix: append([]byte(nil), suffix...),
					},
				},
			},
		}
		p := newPolicy(sigData, pubKeyData)
		httpVal, err := ast.InterfaceToValue(st.Input.HTTP)
		require.NoError(t, err)

		got, err := p.builtinVerifyHTTPPGPSignatureImpl(
			rego.BuiltinContext{Context: t.Context()},
			ast.NewTerm(httpVal),
			ast.StringTerm(sigPath),
			ast.StringTerm(keyPath),
			st,
		)
		require.NoError(t, err)
		require.Equal(t, ast.BooleanTerm(true), got)
		require.Nil(t, st.checksumNeededForSignature)
	})

	t.Run("verify-failure-returns-false", func(t *testing.T) {
		encoded := checksumDigest.Encoded()
		require.NotEmpty(t, encoded)
		flippedFirst := byte('0')
		if encoded[0] == '0' {
			flippedFirst = '1'
		}
		flipped := string(flippedFirst) + encoded[1:]
		badDigest := digest.NewDigestFromEncoded(checksumDigest.Algorithm(), flipped)

		st := &state{
			Input: Input{
				HTTP: &HTTP{
					checksumResponseForSignature: &httpChecksumResponseForSignature{
						Digest: badDigest.String(),
						Suffix: append([]byte(nil), suffix...),
					},
				},
			},
		}
		p := newPolicy(sigData, pubKeyData)
		httpVal, err := ast.InterfaceToValue(st.Input.HTTP)
		require.NoError(t, err)

		got, err := p.builtinVerifyHTTPPGPSignatureImpl(
			rego.BuiltinContext{Context: t.Context()},
			ast.NewTerm(httpVal),
			ast.StringTerm(sigPath),
			ast.StringTerm(keyPath),
			st,
		)
		require.NoError(t, err)
		require.Equal(t, ast.BooleanTerm(false), got)
	})

	t.Run("missing-checksum-response-returns-false-and-adds-unknown", func(t *testing.T) {
		st := &state{
			Input: Input{
				HTTP: &HTTP{},
			},
		}
		p := newPolicy(sigData, pubKeyData)
		httpVal, err := ast.InterfaceToValue(st.Input.HTTP)
		require.NoError(t, err)

		got, err := p.builtinVerifyHTTPPGPSignatureImpl(
			rego.BuiltinContext{Context: t.Context()},
			ast.NewTerm(httpVal),
			ast.StringTerm(sigPath),
			ast.StringTerm(keyPath),
			st,
		)
		require.NoError(t, err)
		require.Equal(t, ast.BooleanTerm(false), got)
		require.Contains(t, st.Unknowns, funcVerifyHTTPPGPSignature)
		require.NotNil(t, st.checksumNeededForSignature)
	})

	t.Run("invalid-signature-errors", func(t *testing.T) {
		p := newPolicy([]byte("not-a-signature"), pubKeyData)
		st := &state{Input: Input{HTTP: &HTTP{}}}
		httpVal, err := ast.InterfaceToValue(st.Input.HTTP)
		require.NoError(t, err)

		got, err := p.builtinVerifyHTTPPGPSignatureImpl(
			rego.BuiltinContext{Context: t.Context()},
			ast.NewTerm(httpVal),
			ast.StringTerm(sigPath),
			ast.StringTerm(keyPath),
			st,
		)
		require.Nil(t, got)
		require.ErrorContains(t, err, "verify_http_pgp_signature: failed to parse detached signature")
	})
}

func createDetachedPGPFixture(t *testing.T, payload []byte) ([]byte, []byte, digest.Digest, []byte) {
	t.Helper()

	entity, err := openpgp.NewEntity("buildx", "", "buildx@example.com", &packet.Config{
		DefaultHash: crypto.SHA256,
		RSABits:     2048,
	})
	require.NoError(t, err)

	var sigBuf bytes.Buffer
	err = openpgp.ArmoredDetachSign(&sigBuf, entity, bytes.NewReader(payload), &packet.Config{
		DefaultHash: crypto.SHA256,
	})
	require.NoError(t, err)
	sigData := sigBuf.Bytes()

	var pubBuf bytes.Buffer
	aw, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	err = entity.Serialize(aw)
	require.NoError(t, err)
	err = aw.Close()
	require.NoError(t, err)
	pubKeyData := pubBuf.Bytes()

	sig, _, err := pgpsign.ParseArmoredDetachedSignature(sigData)
	require.NoError(t, err)

	h := sig.Hash.New()
	_, err = h.Write(payload)
	require.NoError(t, err)
	_, err = h.Write(sig.HashSuffix)
	require.NoError(t, err)
	sum := h.Sum(nil)

	dAlgo := digest.SHA256
	switch sig.Hash {
	case crypto.SHA384:
		dAlgo = digest.SHA384
	case crypto.SHA512:
		dAlgo = digest.SHA512
	}

	return sigData, pubKeyData, digest.NewDigestFromEncoded(dAlgo, hex.EncodeToString(sum)), append([]byte(nil), sig.HashSuffix...)
}
