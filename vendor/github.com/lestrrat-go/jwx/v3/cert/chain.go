package cert

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"

	"github.com/lestrrat-go/jwx/v3/internal/tokens"
)

// Chain represents a certificate chain as used in the `x5c` field of
// various objects within JOSE.
//
// It stores the certificates as a list of base64-encoded byte sequences. Every
// certificate added to or decoded into the chain must parse as X.509 and is
// subject to the global limits configured by `cert.Settings()`.
type Chain struct {
	certificates [][]byte
}

func (cc Chain) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(tokens.OpenSquareBracket)
	for i, cert := range cc.certificates {
		if i > 0 {
			buf.WriteByte(tokens.Comma)
		}
		encoded, err := json.Marshal(string(cert))
		if err != nil {
			return nil, fmt.Errorf(`failed to encode certificate at index %d: %w`, i, err)
		}
		buf.Write(encoded)
	}
	buf.WriteByte(tokens.CloseSquareBracket)
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes an `x5c` JSON array and validates each entry as a
// base64-encoded X.509 certificate.
func (cc *Chain) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf(`failed to unmarshal certificate chain: %w`, err)
	}

	delim, ok := tok.(json.Delim)
	if !ok || delim != '[' {
		return fmt.Errorf(`failed to unmarshal certificate chain: expected JSON array`)
	}

	var certs [][]byte
	for dec.More() {
		if err := validateChainLength(len(certs) + 1); err != nil {
			return fmt.Errorf(`failed to unmarshal certificate chain: %w`, err)
		}

		var cert string
		if err := dec.Decode(&cert); err != nil {
			return fmt.Errorf(`failed to decode certificate at index %d: %w`, len(certs), err)
		}

		normalized, err := normalizeAndValidateChainCertificate([]byte(cert))
		if err != nil {
			return fmt.Errorf(`failed to decode certificate at index %d: %w`, len(certs), err)
		}
		certs = append(certs, normalized)
	}

	tok, err = dec.Token()
	if err != nil {
		return fmt.Errorf(`failed to unmarshal certificate chain: %w`, err)
	}

	delim, ok = tok.(json.Delim)
	if !ok || delim != ']' {
		return fmt.Errorf(`failed to unmarshal certificate chain: expected closing array`)
	}

	if _, err := dec.Token(); err != io.EOF {
		if err != nil {
			return fmt.Errorf(`failed to unmarshal certificate chain: %w`, err)
		}
		return fmt.Errorf(`failed to unmarshal certificate chain: unexpected trailing data`)
	}

	cc.certificates = certs
	return nil
}

// Get returns the n-th ASN.1 DER + base64 encoded certificate
// stored. `false` will be returned in the second argument if
// the corresponding index is out of range.
func (cc *Chain) Get(index int) ([]byte, bool) {
	if index < 0 || index >= len(cc.certificates) {
		return nil, false
	}

	return cc.certificates[index], true
}

// Len returns the number of certificates stored in this Chain
func (cc *Chain) Len() int {
	return len(cc.certificates)
}

func (cc *Chain) AddString(der string) error {
	return cc.Add([]byte(der))
}

// Add appends a certificate to the chain.
//
// Input may be either a PEM `CERTIFICATE` block or a base64-encoded DER value
// as stored in JOSE `x5c` fields. The certificate is validated as X.509 and is
// subject to the global limits configured by `cert.Settings()`.
func (cc *Chain) Add(der []byte) error {
	if err := validateChainLength(len(cc.certificates) + 1); err != nil {
		return fmt.Errorf(`cert.Chain.Add: %w`, err)
	}

	der = bytes.TrimSpace(der)
	// Accept a PEM-encoded CERTIFICATE block and convert it to the
	// base64(DER) form that x5c requires.
	if block, _ := pem.Decode(der); block != nil && block.Type == "CERTIFICATE" {
		if _, err := validateDERCertificate(block.Bytes); err != nil {
			return fmt.Errorf(`cert.Chain.Add: %w`, err)
		}

		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(block.Bytes)))
		base64.StdEncoding.Encode(encoded, block.Bytes)
		cc.certificates = append(cc.certificates, encoded)
		return nil
	}

	// Non-PEM input must be base64(DER). Strip any internal whitespace
	// (callers commonly pass multi-line base64 literals) and validate.
	normalized, err := normalizeAndValidateChainCertificate(der)
	if err != nil {
		return fmt.Errorf(`cert.Chain.Add: %w`, err)
	}
	cc.certificates = append(cc.certificates, normalized)
	return nil
}

func stripASCIIWhitespace(src []byte) []byte {
	dst := make([]byte, 0, len(src))
	for _, b := range src {
		switch b {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			continue
		}
		dst = append(dst, b)
	}
	return dst
}
