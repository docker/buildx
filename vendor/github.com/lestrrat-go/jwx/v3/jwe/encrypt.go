package jwe

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rsa"
	"fmt"

	"github.com/lestrrat-go/jwx/v3/internal/keyconv"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe/internal/keygen"
	"github.com/lestrrat-go/jwx/v3/jwe/jwebb"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// encrypter is responsible for taking various components to encrypt a key.
// its operation is not concurrency safe. You must provide locking yourself
//
//nolint:govet
type encrypter struct {
	apu        []byte
	apv        []byte
	ctalg      jwa.ContentEncryptionAlgorithm
	keyalg     jwa.KeyEncryptionAlgorithm
	pubkey     any
	rawKey     any
	pbes2Count int
}

// newEncrypter creates a new Encrypter instance with all required parameters.
// The content cipher is built internally during construction.
//
// pubkey must be a public key in its "raw" format (i.e. something like
// *rsa.PublicKey, instead of jwk.Key)
//
// You should consider this object immutable once created.
func newEncrypter(keyalg jwa.KeyEncryptionAlgorithm, ctalg jwa.ContentEncryptionAlgorithm, pubkey any, rawKey any, apu, apv []byte, pbes2Count int) *encrypter {
	return &encrypter{
		apu:        apu,
		apv:        apv,
		ctalg:      ctalg,
		keyalg:     keyalg,
		pubkey:     pubkey,
		rawKey:     rawKey,
		pbes2Count: pbes2Count,
	}
}

func (e *encrypter) EncryptKey(cek []byte) (keygen.ByteSource, error) {
	keyalgStr := e.keyalg.String()
	ctalgStr := e.ctalg.String()

	if ke, ok := e.pubkey.(KeyEncrypter); ok {
		encrypted, err := ke.EncryptKey(cek)
		if err != nil {
			return nil, err
		}
		return keygen.ByteKey(encrypted), nil
	}

	if jwebb.IsDirect(keyalgStr) {
		sharedkey, ok := e.rawKey.([]byte)
		if !ok {
			return nil, fmt.Errorf("encrypt key: []byte is required as the key for %s (got %T)", keyalgStr, e.rawKey)
		}
		return jwebb.KeyEncryptDirect(cek, keyalgStr, sharedkey)
	}

	if jwebb.IsPBES2(keyalgStr) {
		password, ok := e.rawKey.([]byte)
		if !ok {
			return nil, fmt.Errorf("encrypt key: []byte is required as the password for %s (got %T)", keyalgStr, e.rawKey)
		}
		return jwebb.KeyEncryptPBES2(cek, keyalgStr, password, e.pbes2Count)
	}

	if jwebb.IsAESGCMKW(keyalgStr) {
		sharedkey, ok := e.rawKey.([]byte)
		if !ok {
			return nil, fmt.Errorf("encrypt key: []byte is required as the key for %s (got %T)", keyalgStr, e.rawKey)
		}
		return jwebb.KeyEncryptAESGCMKW(cek, keyalgStr, sharedkey)
	}

	if jwebb.IsECDHES(keyalgStr) {
		_, keysize, keywrap, err := jwebb.KeyEncryptionECDHESKeySize(keyalgStr, ctalgStr)
		if err != nil {
			return nil, fmt.Errorf(`failed to determine ECDH-ES key size: %w`, err)
		}

		// Use rawKey for ECDH-ES operations - it should contain the actual key material
		keyToUse := e.rawKey
		if keyToUse == nil {
			keyToUse = e.pubkey
		}

		switch key := keyToUse.(type) {
		case *ecdsa.PublicKey:
			// no op
		case ecdsa.PublicKey:
			keyToUse = &key
		case *ecdsa.PrivateKey:
			keyToUse = &key.PublicKey
		case ecdsa.PrivateKey:
			keyToUse = &key.PublicKey
		case *ecdh.PublicKey:
			// no op
		case ecdh.PublicKey:
			keyToUse = &key
		case ecdh.PrivateKey:
			keyToUse = key.PublicKey()
		case *ecdh.PrivateKey:
			keyToUse = key.PublicKey()
		}

		// Determine key type and call appropriate function
		switch key := keyToUse.(type) {
		case *ecdh.PublicKey:
			if key.Curve() == ecdh.X25519() {
				if !keywrap {
					return jwebb.KeyEncryptECDHESX25519(cek, keyalgStr, e.apu, e.apv, key, keysize, ctalgStr)
				}
				return jwebb.KeyEncryptECDHESKeyWrapX25519(cek, keyalgStr, e.apu, e.apv, key, keysize, ctalgStr)
			}

			var ecdsaKey *ecdsa.PublicKey
			if err := keyconv.ECDHToECDSA(&ecdsaKey, key); err != nil {
				return nil, fmt.Errorf(`encrypt: failed to convert ECDH public key to ECDSA: %w`, err)
			}
			keyToUse = ecdsaKey
		}

		switch key := keyToUse.(type) {
		case *ecdsa.PublicKey:
			if !keywrap {
				return jwebb.KeyEncryptECDHESECDSA(cek, keyalgStr, e.apu, e.apv, key, keysize, ctalgStr)
			}
			return jwebb.KeyEncryptECDHESKeyWrapECDSA(cek, keyalgStr, e.apu, e.apv, key, keysize, ctalgStr)
		default:
			return nil, fmt.Errorf(`encrypt: unsupported key type for ECDH-ES: %T`, keyToUse)
		}
	}

	if jwebb.IsRSA15(keyalgStr) {
		keyToUse := e.rawKey
		if keyToUse == nil {
			keyToUse = e.pubkey
		}

		// Handle rsa.PublicKey by value - convert to pointer
		if pk, ok := keyToUse.(rsa.PublicKey); ok {
			keyToUse = &pk
		}

		var pubkey *rsa.PublicKey
		if err := keyconv.RSAPublicKey(&pubkey, keyToUse); err != nil {
			return nil, fmt.Errorf(`encrypt: failed to convert to RSA public key: %w`, err)
		}

		return jwebb.KeyEncryptRSA15(cek, keyalgStr, pubkey)
	}

	if jwebb.IsRSAOAEP(keyalgStr) {
		keyToUse := e.rawKey
		if keyToUse == nil {
			keyToUse = e.pubkey
		}

		// Handle rsa.PublicKey by value - convert to pointer
		if pk, ok := keyToUse.(rsa.PublicKey); ok {
			keyToUse = &pk
		}

		var pubkey *rsa.PublicKey
		if err := keyconv.RSAPublicKey(&pubkey, keyToUse); err != nil {
			return nil, fmt.Errorf(`encrypt: failed to convert to RSA public key: %w`, err)
		}

		return jwebb.KeyEncryptRSAOAEP(cek, keyalgStr, pubkey)
	}

	if jwebb.IsAESKW(keyalgStr) {
		sharedkey, ok := e.rawKey.([]byte)
		if !ok {
			return nil, fmt.Errorf("[]byte is required as the key to encrypt %s", keyalgStr)
		}
		return jwebb.KeyEncryptAESKW(cek, keyalgStr, sharedkey)
	}

	return nil, fmt.Errorf(`unsupported algorithm for key encryption (%s)`, keyalgStr)
}

// validateAlgorithmForKey checks that alg is family-compatible with
// key at the WithKey option boundary, surfacing wrong-shape mismatches
// as crisp `jwe.WithKey: ...` errors instead of nested errors deep in
// the dispatcher (e.g. `[]byte is required as the key to encrypt ...`
// from inside the AESKW path).
//
// Permissive carve-outs (return nil, deferring validation):
//
//   - jwk.Key wrappers: kty-vs-alg check happens at jwk.Export time.
//   - Caller-supplied KeyEncrypter / KeyDecrypter implementations:
//     the caller takes responsibility for the key-shape contract.
//   - Nil key: legitimate for `dir` (caller provides CEK separately).
//
// All other built-in algorithm families enforce a concrete key-shape
// expectation here. The error is wrapped by the WithKey site so the
// caller sees `jwe.WithKey: ...` consistently.
func validateAlgorithmForKey(alg jwa.KeyEncryptionAlgorithm, key any) error {
	if key == nil {
		return nil
	}
	if _, ok := key.(jwk.Key); ok {
		return nil
	}
	if _, ok := key.(KeyEncrypter); ok {
		return nil
	}
	if _, ok := key.(KeyDecrypter); ok {
		return nil
	}

	algStr := alg.String()
	switch {
	case jwebb.IsDirect(algStr):
		if _, ok := key.([]byte); !ok {
			return fmt.Errorf(`algorithm %q requires a []byte key (got %T)`, algStr, key)
		}
	case jwebb.IsAESKW(algStr) || jwebb.IsAESGCMKW(algStr) || jwebb.IsPBES2(algStr):
		if _, ok := key.([]byte); !ok {
			return fmt.Errorf(`algorithm %q requires a []byte key (got %T)`, algStr, key)
		}
	case jwebb.IsRSA15(algStr) || jwebb.IsRSAOAEP(algStr):
		switch key.(type) {
		case *rsa.PublicKey, rsa.PublicKey, *rsa.PrivateKey, rsa.PrivateKey:
		default:
			return fmt.Errorf(`algorithm %q requires an RSA key (got %T)`, algStr, key)
		}
	case jwebb.IsECDHES(algStr):
		switch key.(type) {
		case *ecdsa.PublicKey, ecdsa.PublicKey, *ecdsa.PrivateKey, ecdsa.PrivateKey,
			*ecdh.PublicKey, ecdh.PublicKey, *ecdh.PrivateKey, ecdh.PrivateKey:
		default:
			return fmt.Errorf(`algorithm %q requires an ECDSA or ECDH key (got %T)`, algStr, key)
		}
	default:
		// Unknown algorithm family: defer to dispatch.
		return nil
	}
	return nil
}
