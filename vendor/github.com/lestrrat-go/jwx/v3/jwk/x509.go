package jwk

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"

	"github.com/lestrrat-go/blackmagic"
	"github.com/lestrrat-go/jwx/v3/jwk/jwkbb"
)

// PEMDecoder is an interface to describe an object that can decode
// a key from PEM encoded ASN.1 DER format.
//
// A PEMDecoder can be specified as an option to `jwk.Parse()` or `jwk.ParseKey()`
// along with the `jwk.WithPEM()` option.
type PEMDecoder interface {
	Decode([]byte) (any, []byte, error)
}

// PEMEncoder is an interface to describe an object that can encode
// a key into PEM encoded ASN.1 DER format.
//
// `jwk.Key` instances do not implement a way to encode themselves into
// PEM format. Normally you can just use `jwk.EncodePEM()` to do this, but
// this interface allows you to generalize the encoding process by
// abstracting the `jwk.EncodePEM()` function using `jwk.PEMEncodeFunc`
// along with alternate implementations, should you need them.
type PEMEncoder interface {
	Encode(any) (string, []byte, error)
}

type PEMEncodeFunc func(any) (string, []byte, error)

func (f PEMEncodeFunc) Encode(v any) (string, []byte, error) {
	return f(v)
}

func encodeX509(v any) (string, []byte, error) {
	// we can't import jwk, so just use the interface
	if key, ok := v.(Key); ok {
		var raw any
		if err := Export(key, &raw); err != nil {
			return "", nil, fmt.Errorf(`failed to get raw key out of %T: %w`, key, err)
		}

		v = raw
	}

	// Try to convert it into a certificate
	switch v := v.(type) {
	case *rsa.PrivateKey:
		return pmRSAPrivateKey, x509.MarshalPKCS1PrivateKey(v), nil
	case *ecdsa.PrivateKey:
		marshaled, err := x509.MarshalECPrivateKey(v)
		if err != nil {
			return "", nil, err
		}
		return pmECPrivateKey, marshaled, nil
	case ed25519.PrivateKey:
		marshaled, err := x509.MarshalPKCS8PrivateKey(v)
		if err != nil {
			return "", nil, err
		}
		return pmPrivateKey, marshaled, nil
	case *rsa.PublicKey, *ecdsa.PublicKey, ed25519.PublicKey:
		marshaled, err := x509.MarshalPKIXPublicKey(v)
		if err != nil {
			return "", nil, err
		}
		return pmPublicKey, marshaled, nil
	default:
		return "", nil, fmt.Errorf(`unsupported type %T for ASN.1 DER encoding`, v)
	}
}

// EncodePEM encodes the key into a PEM encoded ASN.1 DER format.
// The key can be a jwk.Key or a raw key instance, but it must be one of
// the types supported by `x509` package.
//
// Internally, it uses the same routine as `jwk.EncodeX509()`, and therefore
// the same caveats apply
func EncodePEM(v any) ([]byte, error) {
	typ, marshaled, err := encodeX509(v)
	if err != nil {
		return nil, fmt.Errorf(`failed to encode key in x509: %w`, err)
	}

	block := &pem.Block{
		Type:  typ,
		Bytes: marshaled,
	}
	return pem.EncodeToMemory(block), nil
}

const (
	pmPrivateKey    = `PRIVATE KEY`
	pmPublicKey     = `PUBLIC KEY`
	pmECPrivateKey  = `EC PRIVATE KEY`
	pmRSAPublicKey  = `RSA PUBLIC KEY`
	pmRSAPrivateKey = `RSA PRIVATE KEY`
)

// NewPEMDecoder returns a PEMDecoder that decodes keys in PEM encoded ASN.1 DER format.
// You can use it as argument to `jwk.WithPEMDecoder()` option.
//
// The use of this function is planned to be deprecated. The plan is to replace the
// `jwk.WithPEMDecoder()` option with globally available custom X509 decoders which
// can be registered via `jwk.RegisterX509Decoder()` function.
func NewPEMDecoder() PEMDecoder {
	return pemDecoder{}
}

type pemDecoder struct{}

// Decode decodes a key in PEM encoded ASN.1 DER format.
// and returns a raw key.
func (pemDecoder) Decode(src []byte) (any, []byte, error) {
	block, rest := pem.Decode(src)
	if block == nil {
		return nil, rest, fmt.Errorf(`failed to decode PEM data`)
	}
	var ret any
	if err := jwkbb.DecodeX509(&ret, block); err != nil {
		return nil, rest, err
	}
	return ret, rest, nil
}

// X509Decoder is an interface that describes an object that can decode
// a PEM encoded ASN.1 DER format into a specific type of key.
//
// This interface is experimental, and may change in the future.
type X509Decoder interface {
	// DecodeX509 decodes the given PEM block into the destination object.
	// The destination object must be a pointer to a type that can hold the
	// decoded key, such as *rsa.PrivateKey, *ecdsa.PrivateKey, etc.
	DecodeX509(dst any, block *pem.Block) error
}

// X509DecodeFunc is a function type that implements the X509Decoder interface.
// It allows you to create a custom X509Decoder by providing a function
// that takes a destination and a PEM block, and returns an error if the decoding fails.
//
// This interface is experimental, and may change in the future.
type X509DecodeFunc func(dst any, block *pem.Block) error

func (f X509DecodeFunc) DecodeX509(dst any, block *pem.Block) error {
	return f(dst, block)
}

// x509Decoders holds every registered decoder keyed by its caller-supplied
// ident. x509DecoderIdents keeps the registration order so decodeX509 tries
// decoders in a stable, deterministic sequence. x509DecoderList is the
// read-optimized snapshot handed out to readers; every mutation replaces it
// with a freshly allocated slice so readers can iterate their captured
// header without any further synchronization.
var muX509Decoders sync.RWMutex
var x509Decoders = map[any]X509Decoder{}
var x509DecoderIdents = []any{}
var x509DecoderList = []X509Decoder{}

type identDefaultX509Decoder struct{}

func init() {
	RegisterX509Decoder(identDefaultX509Decoder{}, X509DecodeFunc(jwkbb.DecodeX509))
}

// RegisterX509Decoder registers a new X509Decoder that can decode PEM encoded ASN.1 DER format.
// Because the decoder could be non-comparable, you must provide an identifier that can be used
// as a map key to identify the decoder.
//
// This function is experimental, and may change in the future.
func RegisterX509Decoder(ident any, decoder X509Decoder) {
	if decoder == nil {
		panic(`jwk.RegisterX509Decoder: decoder cannot be nil`)
	}

	muX509Decoders.Lock()
	defer muX509Decoders.Unlock()
	if _, ok := x509Decoders[ident]; ok {
		return // already registered
	}

	x509Decoders[ident] = decoder
	x509DecoderIdents = append(x509DecoderIdents, ident)
	// Publish a fresh slice so any reader that snapshotted the previous
	// one keeps iterating its own immutable copy.
	next := make([]X509Decoder, len(x509DecoderList)+1)
	copy(next, x509DecoderList)
	next[len(x509DecoderList)] = decoder
	x509DecoderList = next
}

// UnregisterX509Decoder unregisters the X509Decoder identified by the given identifier.
// If the identifier is not registered, it does nothing.
//
// This function is experimental, and may change in the future.
func UnregisterX509Decoder(ident any) {
	muX509Decoders.Lock()
	defer muX509Decoders.Unlock()
	if _, ok := x509Decoders[ident]; !ok {
		return // not registered
	}

	delete(x509Decoders, ident)

	// Rebuild idents and the reader-facing slice as fresh allocations,
	// preserving registration order and filtering the removed ident.
	// Mutating the old slices in place would race with readers in
	// decodeX509 that iterate a captured snapshot without holding RLock.
	nextIdents := make([]any, 0, len(x509DecoderIdents)-1)
	nextList := make([]X509Decoder, 0, len(x509DecoderList)-1)
	for _, id := range x509DecoderIdents {
		if id == ident {
			continue
		}
		nextIdents = append(nextIdents, id)
		nextList = append(nextList, x509Decoders[id])
	}
	x509DecoderIdents = nextIdents
	x509DecoderList = nextList
}

// decodeX509 decodes a PEM encoded ASN.1 DER format into the given destination.
// It tries all registered X509 decoders until one of them succeeds.
// If no decoder can handle the PEM block, it returns an error.
func decodeX509(dst any, src []byte) error {
	block, _ := pem.Decode(src)
	if block == nil {
		return fmt.Errorf(`failed to decode PEM data`)
	}

	muX509Decoders.RLock()
	decoders := x509DecoderList
	muX509Decoders.RUnlock()

	var errs []error
	for _, d := range decoders {
		if err := d.DecodeX509(dst, block); err != nil {
			errs = append(errs, err)
			continue
		}
		// successfully decoded
		return nil
	}

	return fmt.Errorf(`failed to decode X509 data using any of the decoders: %w`, errors.Join(errs...))
}

func decodeX509WithPEMDEcoder(dst any, src []byte, decoder PEMDecoder) error {
	ret, _, err := decoder.Decode(src)
	if err != nil {
		return fmt.Errorf(`failed to decode PEM data: %w`, err)
	}

	if err := blackmagic.AssignIfCompatible(dst, ret); err != nil {
		return fmt.Errorf(`failed to assign decoded key to destination: %w`, err)
	}

	return nil
}
