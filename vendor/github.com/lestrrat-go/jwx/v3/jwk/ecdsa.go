package jwk

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"math/big"
	"reflect"

	"github.com/lestrrat-go/jwx/v3/internal/base64"
	"github.com/lestrrat-go/jwx/v3/internal/ecutil"
	"github.com/lestrrat-go/jwx/v3/jwa"
	ourecdsa "github.com/lestrrat-go/jwx/v3/jwk/ecdsa"
)

func init() {
	ourecdsa.RegisterCurve(jwa.P256(), elliptic.P256())
	ourecdsa.RegisterCurve(jwa.P384(), elliptic.P384())
	ourecdsa.RegisterCurve(jwa.P521(), elliptic.P521())

	RegisterKeyExporter(KeyKind(jwa.EC().String()), KeyExportFunc(ecdsaJWKToRaw))
}

func (k *ecdsaPublicKey) Import(rawKey *ecdsa.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if rawKey.X == nil {
		return fmt.Errorf(`invalid ecdsa.PublicKey`)
	}

	if rawKey.Y == nil {
		return fmt.Errorf(`invalid ecdsa.PublicKey`)
	}

	if err := validateECDSAPoint(rawKey.Curve, rawKey.X, rawKey.Y); err != nil {
		return fmt.Errorf(`jwk: %w`, err)
	}

	xbuf := ecutil.AllocECPointBuffer(rawKey.X, rawKey.Curve)
	ybuf := ecutil.AllocECPointBuffer(rawKey.Y, rawKey.Curve)
	defer ecutil.ReleaseECPointBuffer(xbuf)
	defer ecutil.ReleaseECPointBuffer(ybuf)

	k.x = make([]byte, len(xbuf))
	copy(k.x, xbuf)
	k.y = make([]byte, len(ybuf))
	copy(k.y, ybuf)

	alg, err := ourecdsa.AlgorithmFromCurve(rawKey.Curve)
	if err != nil {
		return fmt.Errorf(`jwk: failed to get algorithm for converting ECDSA public key to JWK: %w`, err)
	}
	k.crv = &alg

	return nil
}

func (k *ecdsaPrivateKey) Import(rawKey *ecdsa.PrivateKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if rawKey.PublicKey.X == nil {
		return fmt.Errorf(`invalid ecdsa.PrivateKey`)
	}
	if rawKey.PublicKey.Y == nil {
		return fmt.Errorf(`invalid ecdsa.PrivateKey`)
	}
	if rawKey.D == nil {
		return fmt.Errorf(`invalid ecdsa.PrivateKey`)
	}

	if err := validateECDSAPoint(rawKey.Curve, rawKey.PublicKey.X, rawKey.PublicKey.Y); err != nil {
		return fmt.Errorf(`jwk: %w`, err)
	}

	xbuf := ecutil.AllocECPointBuffer(rawKey.PublicKey.X, rawKey.Curve)
	ybuf := ecutil.AllocECPointBuffer(rawKey.PublicKey.Y, rawKey.Curve)
	dbuf := ecutil.AllocECPointBuffer(rawKey.D, rawKey.Curve)
	defer ecutil.ReleaseECPointBuffer(xbuf)
	defer ecutil.ReleaseECPointBuffer(ybuf)
	defer ecutil.ReleaseECPointBuffer(dbuf)

	k.x = make([]byte, len(xbuf))
	copy(k.x, xbuf)
	k.y = make([]byte, len(ybuf))
	copy(k.y, ybuf)
	k.d = make([]byte, len(dbuf))
	copy(k.d, dbuf)

	alg, err := ourecdsa.AlgorithmFromCurve(rawKey.Curve)
	if err != nil {
		return fmt.Errorf(`jwk: failed to get algorithm for converting ECDSA private key to JWK: %w`, err)
	}
	k.crv = &alg

	return nil
}

func buildECDSAPublicKey(alg jwa.EllipticCurveAlgorithm, xbuf, ybuf []byte) (*ecdsa.PublicKey, error) {
	crv, err := ourecdsa.CurveFromAlgorithm(alg)
	if err != nil {
		return nil, fmt.Errorf(`jwk: failed to get algorithm for ECDSA public key: %w`, err)
	}

	var x, y big.Int
	x.SetBytes(xbuf)
	y.SetBytes(ybuf)

	if err := validateECDSAPoint(crv, &x, &y); err != nil {
		return nil, fmt.Errorf(`jwk: %w`, err)
	}

	return &ecdsa.PublicKey{Curve: crv, X: &x, Y: &y}, nil
}

// validateECDSAPoint rejects ECDSA public key coordinates that are not
// safe to use: the identity point (0, 0) and any point that does not lie
// on the named curve. Without these checks, attacker-supplied JWKs can
// smuggle off-curve or small-subgroup points into downstream ECDSA/ECDH
// operations (invalid-curve attacks). See JWK-003.
//
// The implementation is split into two branches for a reason:
//
//  1. For the NIST P-256/P-384/P-521 curves we route through crypto/ecdh.
//     Go 1.21 deprecated most of crypto/elliptic's Curve methods — not
//     because point-on-curve validation stopped being necessary, but
//     because the generic big.Int implementation in crypto/elliptic had
//     subtle edge cases and the Go team wanted users off it. The blessed
//     replacement for "parse and validate an uncompressed point" on
//     stdlib curves is ecdh.Curve.NewPublicKey, which enforces on-curve
//     membership and rejects the identity as part of parsing the SEC1
//     0x04 || X || Y encoding. Using ecdh here means we're using exactly
//     the Go team's recommended replacement, and the deprecated stdlib
//     elliptic methods are never reached for any NIST-curve input.
//
//  2. For any other curve registered through jwk/ecdsa.RegisterCurve
//     (most importantly secp256k1 via the ES256K extension module),
//     crypto/ecdh has no entry point — it only supports the four curves
//     listed above. The only mechanism available for validating a point
//     on a custom curve is the elliptic.Curve interface's IsOnCurve
//     method. Calling it here is correct despite the staticcheck
//     deprecation notice, for three reasons:
//
//     a. The deprecation targets the *stdlib* elliptic.Curve
//     implementations (elliptic.P256() etc.). Custom curves such as
//     btcec/secp256k1 ship their own IsOnCurve implementation; the
//     interface dispatch lands in that implementation, not in the
//     deprecated stdlib one. staticcheck cannot see through interface
//     dispatch, so the lint scope is suppressed on just this line.
//
//     b. The elliptic.Curve interface itself remains part of Go's
//     supported API because crypto/ecdsa.Verify and
//     crypto/ecdsa.Sign continue to take elliptic.Curve-backed keys.
//     Any third-party curve that plugs into crypto/ecdsa is
//     contractually required to implement a working IsOnCurve; that
//     is the only thing crypto/ecdsa has to validate the public point
//     before verification. Calling it from here is the same contract.
//
//     c. The remaining alternatives are worse: (i) refusing to validate
//     non-stdlib curves at all reintroduces JWK-003 for ES256K users;
//     (ii) refusing to *support* non-stdlib curves is a regression
//     for ES256K users. A cleaner long-term fix is to extend
//     jwk/ecdsa.RegisterCurve so extension modules can register a
//     validator function alongside the curve, letting us drop the
//     IsOnCurve call entirely. That is a deliberate follow-up, not a
//     blocker for this security fix.
func validateECDSAPoint(crv elliptic.Curve, x, y *big.Int) error {
	if x.Sign() == 0 && y.Sign() == 0 {
		return fmt.Errorf(`invalid ECDSA public key: identity point is not a valid public key`)
	}

	// Coordinates must fit in the curve's field. The NIST P-curve
	// branch below pads x and y into a fixed-size SEC1 buffer via
	// big.Int.FillBytes, which panics on oversized input. Bounding
	// here makes the function safe by construction for every caller,
	// including jwk.Import handed a hand-built *ecdsa.PublicKey from
	// raw bytes.
	bits := crv.Params().BitSize
	if x.BitLen() > bits {
		return fmt.Errorf(`invalid ECDSA public key: x coordinate is %d bits, exceeds curve %q field size of %d bits`, x.BitLen(), crv.Params().Name, bits)
	}
	if y.BitLen() > bits {
		return fmt.Errorf(`invalid ECDSA public key: y coordinate is %d bits, exceeds curve %q field size of %d bits`, y.BitLen(), crv.Params().Name, bits)
	}

	if ecdhCrv, ok := stdlibECDHCurve(crv); ok {
		size := (crv.Params().BitSize + 7) / 8
		buf := make([]byte, 1+2*size)
		buf[0] = 0x04
		x.FillBytes(buf[1 : 1+size])
		y.FillBytes(buf[1+size:])
		if _, err := ecdhCrv.NewPublicKey(buf); err != nil {
			return fmt.Errorf(`invalid ECDSA public key: %w`, err)
		}
		return nil
	}

	// Custom-curve fallback. See the block comment on validateECDSAPoint
	// for the full justification of calling a deprecated-marked method;
	// the short version is that interface dispatch lands in the custom
	// curve's own IsOnCurve, not in deprecated stdlib code.
	if !crv.IsOnCurve(x, y) { //nolint:staticcheck // see validateECDSAPoint godoc: only path that validates custom curves
		return fmt.Errorf(`invalid ECDSA public key: point is not on curve %q`, crv.Params().Name)
	}
	return nil
}

// stdlibECDHCurve maps a crypto/elliptic curve to its crypto/ecdh
// counterpart when one exists. Only the NIST P-curves supported by both
// packages are mapped; everything else returns ok=false and falls back
// to the elliptic.Curve path in validateECDSAPoint.
func stdlibECDHCurve(crv elliptic.Curve) (ecdh.Curve, bool) {
	switch crv {
	case elliptic.P256():
		return ecdh.P256(), true
	case elliptic.P384():
		return ecdh.P384(), true
	case elliptic.P521():
		return ecdh.P521(), true
	}
	return nil, false
}

func buildECDHPublicKey(alg jwa.EllipticCurveAlgorithm, xbuf, ybuf []byte) (*ecdh.PublicKey, error) {
	var ecdhcrv ecdh.Curve
	switch alg {
	case jwa.X25519():
		ecdhcrv = ecdh.X25519()
	case jwa.P256():
		ecdhcrv = ecdh.P256()
	case jwa.P384():
		ecdhcrv = ecdh.P384()
	case jwa.P521():
		ecdhcrv = ecdh.P521()
	default:
		return nil, fmt.Errorf(`jwk: unsupported ECDH curve %s`, alg)
	}

	return ecdhcrv.NewPublicKey(append([]byte{0x04}, append(xbuf, ybuf...)...))
}

func buildECDHPrivateKey(alg jwa.EllipticCurveAlgorithm, dbuf []byte) (*ecdh.PrivateKey, error) {
	var ecdhcrv ecdh.Curve
	switch alg {
	case jwa.X25519():
		ecdhcrv = ecdh.X25519()
	case jwa.P256():
		ecdhcrv = ecdh.P256()
	case jwa.P384():
		ecdhcrv = ecdh.P384()
	case jwa.P521():
		ecdhcrv = ecdh.P521()
	default:
		return nil, fmt.Errorf(`jwk: unsupported ECDH curve %s`, alg)
	}

	return ecdhcrv.NewPrivateKey(dbuf)
}

var ecdsaConvertibleTypes = []reflect.Type{
	reflect.TypeFor[ECDSAPrivateKey](),
	reflect.TypeFor[ECDSAPublicKey](),
}

func ecdsaJWKToRaw(keyif Key, hint any) (any, error) {
	var isECDH bool

	extracted, err := extractEmbeddedKey(keyif, ecdsaConvertibleTypes)
	if err != nil {
		return nil, fmt.Errorf(`jwk: failed to extract embedded key: %w`, err)
	}

	switch k := extracted.(type) {
	case ECDSAPrivateKey:
		switch hint.(type) {
		case ecdsa.PrivateKey, *ecdsa.PrivateKey:
		case ecdh.PrivateKey, *ecdh.PrivateKey:
			isECDH = true
		default:
			rv := reflect.ValueOf(hint)
			//nolint:revive
			if rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Interface {
				// pointer to an interface value, presumably they want us to dynamically
				// create an object of the right type
			} else {
				return nil, fmt.Errorf(`invalid destination object type %T: %w`, hint, ContinueError())
			}
		}

		// rlocker is unexported with unexported methods, so only our
		// concrete types implement it. A successful assertion lets us
		// type-assert to the concrete struct and read fields directly
		// under a single batch lock. This avoids nested RLock (which
		// deadlocks when a writer is pending) while preserving an
		// atomic snapshot of all fields.
		var crv jwa.EllipticCurveAlgorithm
		var hasCrv bool
		var od, ox, oy []byte
		if locker, ok := k.(rlocker); ok {
			locker.rlock()
			concrete := k.(*ecdsaPrivateKey) //nolint:forcetypeassert // rlocker is unexported; only our concrete types implement it
			if concrete.crv != nil {
				crv = *(concrete.crv)
				hasCrv = true
			}
			od, ox, oy = concrete.d, concrete.x, concrete.y
			locker.runlock()
		} else {
			// External implementation — use self-locking interface getters.
			var ok bool
			if crv, ok = k.Crv(); !ok {
				return nil, fmt.Errorf(`missing "crv" field`)
			}
			hasCrv = true
			if od, ok = k.D(); !ok {
				return nil, fmt.Errorf(`missing "d" field`)
			}
			if ox, ok = k.X(); !ok {
				return nil, fmt.Errorf(`missing "x" field`)
			}
			if oy, ok = k.Y(); !ok {
				return nil, fmt.Errorf(`missing "y" field`)
			}
		}

		if !hasCrv {
			return nil, fmt.Errorf(`missing "crv" field`)
		}

		if isECDH {
			if od == nil {
				return nil, fmt.Errorf(`missing "d" field`)
			}
			return buildECDHPrivateKey(crv, od)
		}

		if ox == nil {
			return nil, fmt.Errorf(`missing "x" field`)
		}
		if oy == nil {
			return nil, fmt.Errorf(`missing "y" field`)
		}
		if od == nil {
			return nil, fmt.Errorf(`missing "d" field`)
		}

		pubk, err := buildECDSAPublicKey(crv, ox, oy)
		if err != nil {
			return nil, fmt.Errorf(`failed to build public key: %w`, err)
		}

		var key ecdsa.PrivateKey
		var d big.Int

		d.SetBytes(od)
		key.D = &d
		key.PublicKey = *pubk

		return &key, nil
	case ECDSAPublicKey:
		switch hint.(type) {
		case ecdsa.PublicKey, *ecdsa.PublicKey:
		case ecdh.PublicKey, *ecdh.PublicKey:
			isECDH = true
		default:
			rv := reflect.ValueOf(hint)
			//nolint:revive
			if rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Interface {
				// pointer to an interface value, presumably they want us to dynamically
				// create an object of the right type
			} else {
				return nil, fmt.Errorf(`invalid destination object type %T: %w`, hint, ContinueError())
			}
		}

		// See ECDSAPrivateKey case above for explanation of the rlocker pattern.
		var crv jwa.EllipticCurveAlgorithm
		var hasCrv bool
		var x, y []byte
		if locker, ok := k.(rlocker); ok {
			locker.rlock()
			concrete := k.(*ecdsaPublicKey) //nolint:forcetypeassert // rlocker is unexported; only our concrete types implement it
			if concrete.crv != nil {
				crv = *(concrete.crv)
				hasCrv = true
			}
			x, y = concrete.x, concrete.y
			locker.runlock()
		} else {
			var ok bool
			if crv, ok = k.Crv(); !ok {
				return nil, fmt.Errorf(`missing "crv" field`)
			}
			hasCrv = true
			if x, ok = k.X(); !ok {
				return nil, fmt.Errorf(`missing "x" field`)
			}
			if y, ok = k.Y(); !ok {
				return nil, fmt.Errorf(`missing "y" field`)
			}
		}

		if !hasCrv {
			return nil, fmt.Errorf(`missing "crv" field`)
		}
		if x == nil {
			return nil, fmt.Errorf(`missing "x" field`)
		}
		if y == nil {
			return nil, fmt.Errorf(`missing "y" field`)
		}
		if isECDH {
			return buildECDHPublicKey(crv, x, y)
		}
		return buildECDSAPublicKey(crv, x, y)
	default:
		return nil, ContinueError()
	}
}

func makeECDSAPublicKey(src Key) (Key, error) {
	newKey := newECDSAPublicKey()

	// Iterate and copy everything except for the bits that should not be in the public key
	for _, k := range src.Keys() {
		switch k {
		case ECDSADKey:
			continue
		default:
			var v any
			if err := src.Get(k, &v); err != nil {
				return nil, fmt.Errorf(`ecdsa: makeECDSAPublicKey: failed to get field %q: %w`, k, err)
			}

			if err := newKey.Set(k, v); err != nil {
				return nil, fmt.Errorf(`ecdsa: makeECDSAPublicKey: failed to set field %q: %w`, k, err)
			}
		}
	}

	return newKey, nil
}

func (k *ecdsaPrivateKey) PublicKey() (Key, error) {
	return makeECDSAPublicKey(k)
}

func (k *ecdsaPublicKey) PublicKey() (Key, error) {
	return makeECDSAPublicKey(k)
}

func ecdsaThumbprint(hash crypto.Hash, crv, x, y string) []byte {
	h := hash.New()
	fmt.Fprint(h, `{"crv":"`)
	fmt.Fprint(h, crv)
	fmt.Fprint(h, `","kty":"EC","x":"`)
	fmt.Fprint(h, x)
	fmt.Fprint(h, `","y":"`)
	fmt.Fprint(h, y)
	fmt.Fprint(h, `"}`)
	return h.Sum(nil)
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (k *ecdsaPublicKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	var key ecdsa.PublicKey
	if err := Export(k, &key); err != nil {
		return nil, fmt.Errorf(`failed to export ecdsa.PublicKey for thumbprint generation: %w`, err)
	}

	xbuf := ecutil.AllocECPointBuffer(key.X, key.Curve)
	ybuf := ecutil.AllocECPointBuffer(key.Y, key.Curve)
	defer ecutil.ReleaseECPointBuffer(xbuf)
	defer ecutil.ReleaseECPointBuffer(ybuf)

	return ecdsaThumbprint(
		hash,
		key.Curve.Params().Name,
		base64.EncodeToString(xbuf),
		base64.EncodeToString(ybuf),
	), nil
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (k *ecdsaPrivateKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	var key ecdsa.PrivateKey
	if err := Export(k, &key); err != nil {
		return nil, fmt.Errorf(`failed to export ecdsa.PrivateKey for thumbprint generation: %w`, err)
	}

	xbuf := ecutil.AllocECPointBuffer(key.X, key.Curve)
	ybuf := ecutil.AllocECPointBuffer(key.Y, key.Curve)
	defer ecutil.ReleaseECPointBuffer(xbuf)
	defer ecutil.ReleaseECPointBuffer(ybuf)

	return ecdsaThumbprint(
		hash,
		key.Curve.Params().Name,
		base64.EncodeToString(xbuf),
		base64.EncodeToString(ybuf),
	), nil
}

func ecdsaValidateKey(k interface {
	Crv() (jwa.EllipticCurveAlgorithm, bool)
	X() ([]byte, bool)
	Y() ([]byte, bool)
}, checkPrivate bool) error {
	crvtyp, ok := k.Crv()
	if !ok {
		return fmt.Errorf(`missing "crv" field`)
	}

	crv, err := ourecdsa.CurveFromAlgorithm(crvtyp)
	if err != nil {
		return fmt.Errorf(`invalid curve algorithm %q: %w`, crvtyp, err)
	}

	keySize := ecutil.CalculateKeySize(crv)
	xbuf, ok := k.X()
	if !ok || len(xbuf) != keySize {
		return fmt.Errorf(`invalid "x" length (%d) for curve %q`, len(xbuf), crv.Params().Name)
	}

	ybuf, ok := k.Y()
	if !ok || len(ybuf) != keySize {
		return fmt.Errorf(`invalid "y" length (%d) for curve %q`, len(ybuf), crv.Params().Name)
	}

	var x, y big.Int
	x.SetBytes(xbuf)
	y.SetBytes(ybuf)
	if err := validateECDSAPoint(crv, &x, &y); err != nil {
		return err
	}

	if checkPrivate {
		if priv, ok := k.(keyWithD); ok {
			if d, ok := priv.D(); !ok || len(d) != keySize {
				return fmt.Errorf(`invalid "d" length (%d) for curve %q`, len(d), crv.Params().Name)
			}
		} else {
			return fmt.Errorf(`missing "d" value`)
		}
	}
	return nil
}

func (k *ecdsaPrivateKey) Validate() error {
	if err := ecdsaValidateKey(k, true); err != nil {
		return NewKeyValidationError(fmt.Errorf(`jwk.ECDSAPrivateKey: %w`, err))
	}
	return nil
}

func (k *ecdsaPublicKey) Validate() error {
	if err := ecdsaValidateKey(k, false); err != nil {
		return NewKeyValidationError(fmt.Errorf(`jwk.ECDSAPublicKey: %w`, err))
	}
	return nil
}
