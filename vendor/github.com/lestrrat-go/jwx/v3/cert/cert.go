package cert

import (
	"bytes"
	"crypto/x509"
	stdlibb64 "encoding/base64"
	"fmt"
	"io"

	"github.com/lestrrat-go/jwx/v3/internal/base64"
)

// Create is a wrapper around x509.CreateCertificate, but it additionally
// encodes it in base64 so that it can be easily added to `x5c` fields
func Create(rand io.Reader, template, parent *x509.Certificate, pub, priv any) ([]byte, error) {
	der, err := x509.CreateCertificate(rand, template, parent, pub, priv)
	if err != nil {
		return nil, fmt.Errorf(`failed to create x509 certificate: %w`, err)
	}
	return EncodeBase64(der)
}

// EncodeBase64 is a utility function to encode ASN.1 DER certificates
// using base64 encoding. This operation is normally done by `pem.Encode`
// but since PEM would include the markers (`-----BEGIN`, and the like)
// while `x5c` fields do not need this, this function can be used to
// shave off a few lines
func EncodeBase64(der []byte) ([]byte, error) {
	enc := stdlibb64.StdEncoding
	dst := make([]byte, enc.EncodedLen(len(der)))
	enc.Encode(dst, der)
	return dst, nil
}

// Parse decodes a base64-encoded ASN.1 DER certificate and validates that it
// parses as X.509.
//
// The certificate must be in PKIX format and it must not contain PEM markers.
// The maximum decoded certificate size is controlled by `cert.Settings()`.
func Parse(src []byte) (*x509.Certificate, error) {
	src = stripASCIIWhitespace(bytes.TrimSpace(src))
	if err := validateEncodedCertificateSize(src); err != nil {
		return nil, err
	}

	dst, err := base64.Decode(src)
	if err != nil {
		return nil, fmt.Errorf(`failed to base64 decode the certificate: %w`, err)
	}

	return validateDERCertificate(dst)
}

func validateDERCertificate(der []byte) (*x509.Certificate, error) {
	if err := validateCertificateSize(len(der)); err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf(`failed to parse x509 certificate: %w`, err)
	}
	return cert, nil
}

func validateEncodedCertificateSize(src []byte) error {
	return validateCertificateSize(decodedCertificateSize(src))
}

func decodedCertificateSize(src []byte) int {
	n := len(src)
	if n == 0 {
		return 0
	}

	size := n / 4 * 3
	switch n % 4 {
	case 2:
		size++
	case 3:
		size += 2
	}

	if n%4 == 0 && src[n-1] == '=' {
		size--
		if n > 1 && src[n-2] == '=' {
			size--
		}
	}

	if size < 0 {
		return 0
	}
	return size
}

func normalizeAndValidateChainCertificate(src []byte) ([]byte, error) {
	normalized := stripASCIIWhitespace(src)
	if err := validateEncodedCertificateSize(normalized); err != nil {
		return nil, err
	}

	der, err := stdlibb64.StdEncoding.DecodeString(string(normalized))
	if err != nil {
		return nil, fmt.Errorf(`failed to base64 decode the certificate: %w`, err)
	}

	if _, err := validateDERCertificate(der); err != nil {
		return nil, err
	}
	return normalized, nil
}
