package jwk

import (
	"crypto"
	"crypto/rsa"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/bits"
	"reflect"
	"sync/atomic"

	"github.com/lestrrat-go/jwx/v3/internal/base64"
	"github.com/lestrrat-go/jwx/v3/internal/pool"
	"github.com/lestrrat-go/jwx/v3/jwa"
)

func init() {
	RegisterKeyExporter(KeyKind(jwa.RSA().String()), KeyExportFunc(rsaJWKToRaw))
}

const minRSAModulusBits = 2048
const minRSAPublicExponent = 3

var rsaMinModulusBits = atomic.Int64{}
var rsaMinPublicExponent atomic.Pointer[big.Int]

func init() {
	rsaMinModulusBits.Store(minRSAModulusBits)
	setMinRSAPublicExponent(minRSAPublicExponent)
}

func setMinRSAPublicExponent(v int) {
	if v <= 0 {
		rsaMinPublicExponent.Store(nil)
		return
	}

	rsaMinPublicExponent.Store(big.NewInt(int64(v)))
}

func (k *rsaPrivateKey) Import(rawKey *rsa.PrivateKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	d, err := bigIntToBytes(rawKey.D)
	if err != nil {
		return fmt.Errorf(`invalid rsa.PrivateKey: %w`, err)
	}
	k.d = d

	l := len(rawKey.Primes)

	if l < 0 /* I know, I'm being paranoid */ || l > 2 {
		return fmt.Errorf(`invalid number of primes in rsa.PrivateKey: need 0 to 2, but got %d`, len(rawKey.Primes))
	}

	if l > 0 {
		p, err := bigIntToBytes(rawKey.Primes[0])
		if err != nil {
			return fmt.Errorf(`invalid rsa.PrivateKey: %w`, err)
		}
		k.p = p
	}

	if l > 1 {
		q, err := bigIntToBytes(rawKey.Primes[1])
		if err != nil {
			return fmt.Errorf(`invalid rsa.PrivateKey: %w`, err)
		}
		k.q = q
	}

	// dp, dq, qi are optional values
	if v, err := bigIntToBytes(rawKey.Precomputed.Dp); err == nil {
		k.dp = v
	}
	if v, err := bigIntToBytes(rawKey.Precomputed.Dq); err == nil {
		k.dq = v
	}
	if v, err := bigIntToBytes(rawKey.Precomputed.Qinv); err == nil {
		k.qi = v
	}

	// public key part
	n, e, err := importRsaPublicKeyByteValues(&rawKey.PublicKey)
	if err != nil {
		return fmt.Errorf(`invalid rsa.PrivateKey: %w`, err)
	}
	k.n = n
	k.e = e

	return nil
}

func importRsaPublicKeyByteValues(rawKey *rsa.PublicKey) ([]byte, []byte, error) {
	n, err := bigIntToBytes(rawKey.N)
	if err != nil {
		return nil, nil, fmt.Errorf(`invalid rsa.PublicKey: %w`, err)
	}
	if rawKey.E <= 0 {
		return nil, nil, fmt.Errorf(`invalid rsa.PublicKey: invalid rsa public exponent: must be a positive odd integer`)
	}

	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(rawKey.E))
	i := 0
	for ; i < len(data); i++ {
		if data[i] != 0x0 {
			break
		}
	}
	return n, data[i:], nil
}

func (k *rsaPublicKey) Import(rawKey *rsa.PublicKey) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	n, e, err := importRsaPublicKeyByteValues(rawKey)
	if err != nil {
		return fmt.Errorf(`invalid rsa.PrivateKey: %w`, err)
	}
	k.n = n
	k.e = e

	return nil
}

func validateRSAModulusAndExponent(n, e []byte) (*big.Int, error) {
	n = trimLeadingZeroBytes(n)
	if len(n) == 0 {
		return nil, fmt.Errorf(`missing "n" value`)
	}

	bigN := new(big.Int).SetBytes(n)
	minBits := int(rsaMinModulusBits.Load())
	if minBits > 0 && bigN.BitLen() < minBits {
		return nil, fmt.Errorf(`rsa modulus too small: got %d bits, need at least %d`, bigN.BitLen(), minBits)
	}

	e = trimLeadingZeroBytes(e)
	if len(e) == 0 {
		return nil, fmt.Errorf(`missing "e" value`)
	}

	bigE := new(big.Int).SetBytes(e)
	minExponent := rsaMinPublicExponent.Load()
	if bigE.Sign() <= 0 || bigE.Bit(0) == 0 {
		return nil, fmt.Errorf(`invalid rsa public exponent: must be a positive odd integer`)
	}
	if minExponent != nil && bigE.Cmp(minExponent) < 0 {
		return nil, fmt.Errorf(`invalid rsa public exponent: got %s, need at least %s`, bigE.String(), minExponent.String())
	}

	// rsa.PublicKey.E is a Go int. Reject exponents that do not fit on the
	// current platform (e.g. GOARCH=386). Without this guard, Int64()/int()
	// silently truncates, causing the materialized key to disagree with the
	// JSON bytes and breaking RFC 7638 thumbprint uniqueness.
	if bigE.BitLen() >= bits.UintSize {
		return nil, fmt.Errorf(`rsa public exponent too large for this platform: %d bits (max %d)`, bigE.BitLen(), bits.UintSize-1)
	}

	return bigE, nil
}

func buildRSAPublicKey(key *rsa.PublicKey, n, e []byte) error {
	bigE, err := validateRSAModulusAndExponent(n, e)
	if err != nil {
		return err
	}
	key.N = new(big.Int).SetBytes(trimLeadingZeroBytes(n))
	key.E = int(bigE.Int64())
	return nil
}

var rsaConvertibleKeys = []reflect.Type{
	reflect.TypeFor[RSAPrivateKey](),
	reflect.TypeFor[RSAPublicKey](),
}

func rsaJWKToRaw(key Key, hint any) (any, error) {
	extracted, err := extractEmbeddedKey(key, rsaConvertibleKeys)
	if err != nil {
		return nil, fmt.Errorf(`failed to extract embedded key: %w`, err)
	}
	switch key := extracted.(type) {
	case RSAPrivateKey:
		switch hint.(type) {
		case *rsa.PrivateKey, *any:
		default:
			return nil, fmt.Errorf(`invalid destination object type %T for private RSA JWK: %w`, hint, ContinueError())
		}

		// rlocker is unexported with unexported methods, so only our
		// concrete types implement it. A successful assertion lets us
		// type-assert to the concrete struct and read fields directly
		// under a single batch lock. This avoids nested RLock (which
		// deadlocks when a writer is pending) while preserving an
		// atomic snapshot of all fields.
		var od, oq, op, on, oe []byte
		var odp, odq, oqi []byte
		var hasDp, hasDq, hasQi bool
		if locker, ok := key.(rlocker); ok {
			locker.rlock()
			concrete := key.(*rsaPrivateKey) //nolint:forcetypeassert // rlocker is unexported; only our concrete types implement it
			od, oq, op, on, oe = concrete.d, concrete.q, concrete.p, concrete.n, concrete.e
			if concrete.dp != nil {
				odp, hasDp = concrete.dp, true
			}
			if concrete.dq != nil {
				odq, hasDq = concrete.dq, true
			}
			if concrete.qi != nil {
				oqi, hasQi = concrete.qi, true
			}
			locker.runlock()
		} else {
			// External implementation — use self-locking interface getters.
			var ok bool
			if od, ok = key.D(); !ok {
				return nil, fmt.Errorf(`missing "d" value`)
			}
			if oq, ok = key.Q(); !ok {
				return nil, fmt.Errorf(`missing "q" value`)
			}
			if op, ok = key.P(); !ok {
				return nil, fmt.Errorf(`missing "p" value`)
			}
			if on, ok = key.N(); !ok {
				return nil, fmt.Errorf(`missing "n" value`)
			}
			if oe, ok = key.E(); !ok {
				return nil, fmt.Errorf(`missing "e" value`)
			}
			odp, hasDp = key.DP()
			odq, hasDq = key.DQ()
			oqi, hasQi = key.QI()
		}

		if od == nil {
			return nil, fmt.Errorf(`missing "d" value`)
		}
		if oq == nil {
			return nil, fmt.Errorf(`missing "q" value`)
		}
		if op == nil {
			return nil, fmt.Errorf(`missing "p" value`)
		}
		if on == nil {
			return nil, fmt.Errorf(`missing "n" value`)
		}
		if oe == nil {
			return nil, fmt.Errorf(`missing "e" value`)
		}

		var d, q, p big.Int // note: do not use from sync.Pool

		d.SetBytes(od)
		q.SetBytes(oq)
		p.SetBytes(op)

		var dp, dq, qi *big.Int

		if hasDp {
			dp = &big.Int{} // note: do not use from sync.Pool
			dp.SetBytes(odp)
		}

		if hasDq {
			dq = &big.Int{} // note: do not use from sync.Pool
			dq.SetBytes(odq)
		}

		if hasQi {
			qi = &big.Int{} // note: do not use from sync.Pool
			qi.SetBytes(oqi)
		}

		var privkey rsa.PrivateKey
		if err := buildRSAPublicKey(&privkey.PublicKey, on, oe); err != nil {
			return nil, fmt.Errorf(`failed to build rsa.PublicKey: %w`, err)
		}
		privkey.D = &d
		privkey.Primes = []*big.Int{&p, &q}

		if dp != nil {
			privkey.Precomputed.Dp = dp
		}
		if dq != nil {
			privkey.Precomputed.Dq = dq
		}
		if qi != nil {
			privkey.Precomputed.Qinv = qi
		}
		// This may look like a no-op, but it's required if we want to
		// compare it against a key generated by rsa.GenerateKey
		privkey.Precomputed.CRTValues = []rsa.CRTValue{}
		return &privkey, nil
	case RSAPublicKey:
		switch hint.(type) {
		case *rsa.PublicKey, *any:
		default:
			return nil, fmt.Errorf(`invalid destination object type %T for public RSA JWK: %w`, hint, ContinueError())
		}

		var n, e []byte
		// See RSAPrivateKey case above for explanation of the rlocker pattern.
		if locker, ok := key.(rlocker); ok {
			locker.rlock()
			concrete := key.(*rsaPublicKey) //nolint:forcetypeassert // rlocker is unexported; only our concrete types implement it
			n, e = concrete.n, concrete.e
			locker.runlock()
		} else {
			var ok bool
			if n, ok = key.N(); !ok {
				return nil, fmt.Errorf(`missing "n" value`)
			}
			if e, ok = key.E(); !ok {
				return nil, fmt.Errorf(`missing "e" value`)
			}
		}

		if n == nil {
			return nil, fmt.Errorf(`missing "n" value`)
		}
		if e == nil {
			return nil, fmt.Errorf(`missing "e" value`)
		}

		var pubkey rsa.PublicKey
		if err := buildRSAPublicKey(&pubkey, n, e); err != nil {
			return nil, fmt.Errorf(`failed to build rsa.PublicKey: %w`, err)
		}

		return &pubkey, nil

	default:
		return nil, ContinueError()
	}
}

func makeRSAPublicKey(src Key) (Key, error) {
	newKey := newRSAPublicKey()

	// Iterate and copy everything except for the bits that should not be in the public key
	for _, k := range src.Keys() {
		switch k {
		case RSADKey, RSADPKey, RSADQKey, RSAPKey, RSAQKey, RSAQIKey:
			continue
		default:
			var v any
			if err := src.Get(k, &v); err != nil {
				return nil, fmt.Errorf(`rsa: makeRSAPublicKey: failed to get field %q: %w`, k, err)
			}
			if err := newKey.Set(k, v); err != nil {
				return nil, fmt.Errorf(`rsa: makeRSAPublicKey: failed to set field %q: %w`, k, err)
			}
		}
	}

	return newKey, nil
}

func (k *rsaPrivateKey) PublicKey() (Key, error) {
	return makeRSAPublicKey(k)
}

func (k *rsaPublicKey) PublicKey() (Key, error) {
	return makeRSAPublicKey(k)
}

// Thumbprint returns the JWK thumbprint using the indicated
// hashing algorithm, according to RFC 7638
func (k *rsaPrivateKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	return rsaThumbprint(hash, k.n, k.e)
}

func (k *rsaPublicKey) Thumbprint(hash crypto.Hash) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	return rsaThumbprint(hash, k.n, k.e)
}

// trimLeadingZeroBytes strips leading zero bytes. RFC 7638 requires the
// minimal big-endian representation of n/e for canonical JSON.
func trimLeadingZeroBytes(b []byte) []byte {
	for len(b) > 0 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

func rsaThumbprint(hash crypto.Hash, n, e []byte) ([]byte, error) {
	n = trimLeadingZeroBytes(n)
	e = trimLeadingZeroBytes(e)
	if len(n) == 0 {
		return nil, fmt.Errorf(`failed to compute rsa thumbprint: missing "n" value`)
	}
	if len(e) == 0 {
		return nil, fmt.Errorf(`failed to compute rsa thumbprint: missing "e" value`)
	}

	buf := pool.BytesBuffer().Get()
	defer pool.BytesBuffer().Put(buf)

	buf.WriteString(`{"e":"`)
	buf.WriteString(base64.EncodeToString(e))
	buf.WriteString(`","kty":"RSA","n":"`)
	buf.WriteString(base64.EncodeToString(n))
	buf.WriteString(`"}`)

	h := hash.New()
	if _, err := buf.WriteTo(h); err != nil {
		return nil, fmt.Errorf(`failed to write rsaThumbprint: %w`, err)
	}
	return h.Sum(nil), nil
}

func validateRSAKey(key interface {
	N() ([]byte, bool)
	E() ([]byte, bool)
}, checkPrivate bool) error {
	n, ok := key.N()
	if !ok {
		return fmt.Errorf(`missing "n" value`)
	}

	e, ok := key.E()
	if !ok {
		return fmt.Errorf(`missing "e" value`)
	}
	if _, err := validateRSAModulusAndExponent(n, e); err != nil {
		return err
	}
	if checkPrivate {
		if priv, ok := key.(keyWithD); ok {
			if d, ok := priv.D(); !ok || len(d) == 0 {
				return fmt.Errorf(`missing "d" value`)
			}
		} else {
			return fmt.Errorf(`missing "d" value`)
		}
	}

	return nil
}

func (k *rsaPrivateKey) Validate() error {
	if err := validateRSAKey(k, true); err != nil {
		return NewKeyValidationError(fmt.Errorf(`jwk.RSAPrivateKey: %w`, err))
	}
	return nil
}

func (k *rsaPublicKey) Validate() error {
	if err := validateRSAKey(k, false); err != nil {
		return NewKeyValidationError(fmt.Errorf(`jwk.RSAPublicKey: %w`, err))
	}
	return nil
}
