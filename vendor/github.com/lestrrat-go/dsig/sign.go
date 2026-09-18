package dsig

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
)

// Sign generates a digital signature using the specified key and algorithm.
//
// rr is an io.Reader that provides randomness for signing. If rr is nil, it defaults to rand.Reader.
// Not all algorithms require this parameter, but it is included for consistency.
// 99% of the time, you can pass nil for rr, and it will work fine.
func Sign(key any, alg string, payload []byte, rr io.Reader) ([]byte, error) {
	info, ok := GetAlgorithmInfo(alg)
	if !ok {
		return nil, fmt.Errorf(`dsig.Sign: unsupported signature algorithm %q`, alg)
	}

	switch info.Family {
	case HMAC:
		return dispatchHMACSign(key, info, payload)
	case RSA:
		return dispatchRSASign(key, info, payload, rr)
	case ECDSA:
		return dispatchECDSASign(key, info, payload, rr)
	case EdDSAFamily:
		return dispatchEdDSASign(key, info, payload, rr)
	case Custom:
		return dispatchCustomSign(key, info, payload, rr)
	default:
		return nil, fmt.Errorf(`dsig.Sign: unsupported signature family %q`, info.Family)
	}
}

func dispatchHMACSign(key any, info AlgorithmInfo, payload []byte) ([]byte, error) {
	meta, ok := info.Meta.(HMACFamilyMeta)
	if !ok {
		return nil, fmt.Errorf(`dsig.Sign: invalid HMAC metadata`)
	}

	var hmackey []byte
	if err := toHMACKey(&hmackey, key); err != nil {
		return nil, fmt.Errorf(`dsig.Sign: %w`, err)
	}
	return SignHMAC(hmackey, payload, meta.HashFunc)
}

func dispatchRSASign(key any, info AlgorithmInfo, payload []byte, rr io.Reader) ([]byte, error) {
	meta, ok := info.Meta.(RSAFamilyMeta)
	if !ok {
		return nil, fmt.Errorf(`dsig.Sign: invalid RSA metadata`)
	}

	cs, isCryptoSigner, err := rsaGetSignerCryptoSignerKey(key)
	if err != nil {
		return nil, fmt.Errorf(`dsig.Sign: %w`, err)
	}
	if isCryptoSigner {
		var options crypto.SignerOpts = meta.Hash
		if meta.PSS {
			rsaopts := rsaPSSOptions(meta.Hash)
			options = &rsaopts
		}
		return SignCryptoSigner(cs, payload, meta.Hash, options, rr)
	}

	privkey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf(`dsig.Sign: invalid key type %T. *rsa.PrivateKey is required`, key)
	}
	return SignRSA(privkey, payload, meta.Hash, meta.PSS, rr)
}

func dispatchEdDSASign(key any, _ AlgorithmInfo, payload []byte, rr io.Reader) ([]byte, error) {
	signer, err := eddsaGetSigner(key)
	if err != nil {
		return nil, fmt.Errorf(`dsig.Sign: %w`, err)
	}

	return SignCryptoSigner(signer, payload, crypto.Hash(0), crypto.Hash(0), rr)
}

func dispatchECDSASign(key any, info AlgorithmInfo, payload []byte, rr io.Reader) ([]byte, error) {
	meta, ok := info.Meta.(ECDSAFamilyMeta)
	if !ok {
		return nil, fmt.Errorf(`dsig.Sign: invalid ECDSA metadata`)
	}

	privkey, cs, isCryptoSigner, err := ecdsaGetSignerKey(key)
	if err != nil {
		return nil, fmt.Errorf(`dsig.Sign: %w`, err)
	}
	if isCryptoSigner {
		return SignECDSACryptoSigner(cs, payload, meta.Hash, rr)
	}
	return SignECDSA(privkey, payload, meta.Hash, rr)
}

func dispatchCustomSign(key any, info AlgorithmInfo, payload []byte, rr io.Reader) ([]byte, error) {
	signer, ok := info.Meta.(Signer)
	if !ok {
		return nil, fmt.Errorf(`dsig.Sign: algorithm has no signer registered`)
	}
	return signer.Sign(key, payload, rr)
}

// SignDigest generates a digital signature from a pre-computed digest.
//
// For RSA/ECDSA, digest is the hash of the signing input and key is the
// private key used for signing.
//
// For HMAC, the digest must be the pre-computed MAC (i.e. the output of
// hmac.New(hashFunc, key) after writing the signing input). The digest IS
// the signature, so it is returned as-is.
//
// EdDSA and Custom families are not supported and return an error.
//
// rr is an io.Reader that provides randomness for signing. If rr is nil,
// it defaults to rand.Reader.
func SignDigest(key any, alg string, digest []byte, rr io.Reader) ([]byte, error) {
	info, ok := GetAlgorithmInfo(alg)
	if !ok {
		return nil, fmt.Errorf(`dsig.SignDigest: unsupported signature algorithm %q`, alg)
	}

	switch info.Family {
	case HMAC:
		// The caller already computed the HMAC (which incorporates the key)
		// and passed it as digest. The digest IS the signature.
		return digest, nil
	case RSA:
		return dispatchRSASignDigest(key, info, digest, rr)
	case ECDSA:
		return dispatchECDSASignDigest(key, info, digest, rr)
	case EdDSAFamily:
		return nil, fmt.Errorf(`dsig.SignDigest: EdDSA does not support digest-based signing`)
	case Custom:
		return nil, fmt.Errorf(`dsig.SignDigest: custom algorithms do not support digest-based signing`)
	default:
		return nil, fmt.Errorf(`dsig.SignDigest: unsupported signature family %q`, info.Family)
	}
}

func dispatchRSASignDigest(key any, info AlgorithmInfo, digest []byte, rr io.Reader) ([]byte, error) {
	meta, ok := info.Meta.(RSAFamilyMeta)
	if !ok {
		return nil, fmt.Errorf(`dsig.SignDigest: invalid RSA metadata`)
	}

	if rr == nil {
		rr = rand.Reader
	}

	cs, isCryptoSigner, err := rsaGetSignerCryptoSignerKey(key)
	if err != nil {
		return nil, fmt.Errorf(`dsig.SignDigest: %w`, err)
	}
	if isCryptoSigner {
		var opts crypto.SignerOpts = meta.Hash
		if meta.PSS {
			rsaopts := rsaPSSOptions(meta.Hash)
			opts = &rsaopts
		}
		return cs.Sign(rr, digest, opts)
	}

	privkey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf(`dsig.SignDigest: invalid key type %T. *rsa.PrivateKey is required`, key)
	}
	if meta.PSS {
		rsaopts := rsaPSSOptions(meta.Hash)
		return rsa.SignPSS(rr, privkey, meta.Hash, digest, &rsaopts)
	}
	return rsa.SignPKCS1v15(rr, privkey, meta.Hash, digest)
}

func dispatchECDSASignDigest(key any, info AlgorithmInfo, digest []byte, rr io.Reader) ([]byte, error) {
	meta, ok := info.Meta.(ECDSAFamilyMeta)
	if !ok {
		return nil, fmt.Errorf(`dsig.SignDigest: invalid ECDSA metadata`)
	}

	if rr == nil {
		rr = rand.Reader
	}

	privkey, cs, isCryptoSigner, err := ecdsaGetSignerKey(key)
	if err != nil {
		return nil, fmt.Errorf(`dsig.SignDigest: %w`, err)
	}
	if isCryptoSigner {
		signed, err := cs.Sign(rr, digest, meta.Hash)
		if err != nil {
			return nil, fmt.Errorf(`dsig.SignDigest: failed to sign digest using crypto.Signer: %w`, err)
		}
		return signECDSACryptoSigner(cs, signed)
	}

	r, s, err := ecdsa.Sign(rr, privkey, digest)
	if err != nil {
		return nil, fmt.Errorf(`dsig.SignDigest: failed to sign digest using ecdsa: %w`, err)
	}
	return PackECDSASignature(r, s, privkey.Curve.Params().BitSize)
}

