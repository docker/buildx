package aescbc

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"slices"
	"sync/atomic"

	"github.com/lestrrat-go/jwx/v3/internal/pool"
)

const (
	NonceSize = 16
)

const defaultBufSize int64 = 256 * 1024 * 1024

var maxBufSize atomic.Int64

// errInvalidCiphertext is the single opaque error returned by Hmac.Open for
// every failure mode (pre-MAC structural checks and post-MAC tag mismatch).
// Keeping one value across all paths prevents a structural-vs-cryptographic
// oracle on remote decrypt endpoints.
var errInvalidCiphertext = errors.New("invalid ciphertext")

func init() {
	SetMaxBufferSize(defaultBufSize)
}

func SetMaxBufferSize(siz int64) {
	if siz <= 0 {
		siz = defaultBufSize
	}
	maxBufSize.Store(siz)
}

func pad(buf []byte, n int) []byte {
	rem := n - len(buf)%n
	if rem == 0 {
		return buf
	}

	bufsiz := len(buf) + rem
	mbs := maxBufSize.Load()
	if int64(bufsiz) > mbs {
		panic(fmt.Errorf("failed to allocate buffer"))
	}
	newbuf := make([]byte, bufsiz)
	copy(newbuf, buf)

	for i := len(buf); i < len(newbuf); i++ {
		newbuf[i] = byte(rem)
	}
	return newbuf
}

// ref. https://github.com/golang/go/blob/c3db64c0f45e8f2d75c5b59401e0fc925701b6f4/src/crypto/tls/conn.go#L279-L324
//
// extractPadding returns, in constant time, the length of the padding to remove
// from the end of payload. It also returns a byte which is equal to 255 if the
// padding was valid and 0 otherwise. See RFC 2246, Section 6.2.3.2.
func extractPadding(payload []byte) (toRemove int, good byte) {
	if len(payload) < 1 {
		return 0, 0
	}

	paddingLen := payload[len(payload)-1]
	t := uint(len(payload)) - uint(paddingLen)
	// if len(payload) > paddingLen then the MSB of t is zero
	good = byte(int32(^t) >> 31)

	// The maximum possible padding length plus the actual length field
	toCheck := 256
	// The length of the padded data is public, so we can use an if here
	toCheck = min(toCheck, len(payload))

	for i := 1; i <= toCheck; i++ {
		t := uint(paddingLen) - uint(i)
		// if i <= paddingLen then the MSB of t is zero
		mask := byte(int32(^t) >> 31)
		b := payload[len(payload)-i]
		good &^= mask&paddingLen ^ mask&b
	}

	// We AND together the bits of good and replicate the result across
	// all the bits.
	good &= good << 4
	good &= good << 2
	good &= good << 1
	good = uint8(int8(good) >> 7)

	// Zero the padding length on error. This ensures any unchecked bytes
	// are included in the MAC. Otherwise, an attacker that could
	// distinguish MAC failures from padding failures could mount an attack
	// similar to POODLE in SSL 3.0: given a good ciphertext that uses a
	// full block's worth of padding, replace the final block with another
	// block. If the MAC check passed but the padding check failed, the
	// last byte of that block decrypted to the block size.
	//
	// See also macAndPaddingGood logic below.
	paddingLen &= good

	toRemove = int(paddingLen)
	return
}

type Hmac struct {
	blockCipher  cipher.Block
	hash         func() hash.Hash
	keysize      int
	tlen         int
	integrityKey []byte
}

type BlockCipherFunc func([]byte) (cipher.Block, error)

func New(key []byte, f BlockCipherFunc) (hmac *Hmac, err error) {
	keysize := len(key) / 2
	ikey := key[:keysize]
	ekey := key[keysize:]

	bc, ciphererr := f(ekey)
	if ciphererr != nil {
		err = fmt.Errorf(`failed to execute block cipher function: %w`, ciphererr)
		return
	}

	// Per RFC 7518 §5.2.2.1, T_LEN is the authentication tag length. For the
	// three defined AES-CBC-HMAC variants (A128CBC-HS256, A192CBC-HS384,
	// A256CBC-HS512) T_LEN happens to equal MAC_KEY_LEN (== keysize here),
	// but we track it independently so a future variant with a different
	// T_LEN won't silently mis-truncate the HMAC output.
	var hfunc func() hash.Hash
	var tlen int
	switch keysize {
	case 16: // A128CBC-HS256
		hfunc = sha256.New
		tlen = 16
	case 24: // A192CBC-HS384
		hfunc = sha512.New384
		tlen = 24
	case 32: // A256CBC-HS512
		hfunc = sha512.New
		tlen = 32
	default:
		return nil, fmt.Errorf("unsupported key size %d", keysize)
	}

	return &Hmac{
		blockCipher:  bc,
		hash:         hfunc,
		integrityKey: ikey,
		keysize:      keysize,
		tlen:         tlen,
	}, nil
}

// NonceSize fulfills the crypto.AEAD interface
func (c Hmac) NonceSize() int {
	return NonceSize
}

// Overhead fulfills the crypto.AEAD interface
func (c Hmac) Overhead() int {
	return c.blockCipher.BlockSize() + c.tlen
}

func (c Hmac) ComputeAuthTag(aad, nonce, ciphertext []byte) ([]byte, error) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(len(aad)*8))

	h := hmac.New(c.hash, c.integrityKey)

	// compute the tag
	// no need to check errors because Write never returns an error: https://pkg.go.dev/hash#Hash
	//
	// > Write (via the embedded io.Writer interface) adds more data to the running hash.
	// > It never returns an error.
	h.Write(aad)
	h.Write(nonce)
	h.Write(ciphertext)
	h.Write(buf[:])
	s := h.Sum(nil)
	return s[:c.tlen], nil
}

func ensureSize(dst []byte, n int) []byte {
	// Grow dst by n bytes, preserving its current contents as the prefix.
	// This matches the crypto.AEAD append contract used by Seal/Open.
	if n < 0 {
		panic(fmt.Errorf("failed to allocate buffer"))
	}

	const maxInt = int64(^uint(0) >> 1)
	maxAlloc := min(maxBufSize.Load(), maxInt)

	if int64(len(dst)) > maxAlloc-int64(n) {
		panic(fmt.Errorf("failed to allocate buffer"))
	}

	retlen := len(dst) + n
	dst = slices.Grow(dst, n)
	return dst[:retlen]
}

// Seal fulfills the crypto.AEAD interface
func (c Hmac) Seal(dst, nonce, plaintext, data []byte) []byte {
	ctlen := len(plaintext)
	bufsiz := ctlen + c.Overhead()
	mbs := maxBufSize.Load()

	if int64(bufsiz) > mbs {
		panic(fmt.Errorf("failed to allocate buffer"))
	}
	ciphertext := make([]byte, bufsiz)[:ctlen]
	copy(ciphertext, plaintext)
	ciphertext = pad(ciphertext, c.blockCipher.BlockSize())

	cbc := cipher.NewCBCEncrypter(c.blockCipher, nonce)
	cbc.CryptBlocks(ciphertext, ciphertext)

	authtag, err := c.ComputeAuthTag(data, nonce, ciphertext)
	if err != nil {
		// Hmac implements cipher.AEAD interface. Seal can't return error.
		// But currently it never reach here because of Hmac.ComputeAuthTag doesn't return error.
		panic(fmt.Errorf("failed to seal on hmac: %v", err))
	}

	ret := ensureSize(dst, len(ciphertext)+len(authtag))
	out := ret[len(dst):]
	n := copy(out, ciphertext)
	copy(out[n:], authtag)

	return ret
}

// Open fulfills the crypto.AEAD interface
func (c Hmac) Open(dst, nonce, ciphertext, data []byte) ([]byte, error) {
	// Validate the IV length explicitly instead of letting
	// cipher.NewCBCDecrypter panic on a mismatched nonce. The caller in
	// jwe/internal/cipher also wraps Open in a defer/recover, and we
	// intentionally keep BOTH layers: the explicit check turns a malformed
	// IV into a normal error on the happy path (reviewable, testable, no
	// stack unwind), while the recover stays as a belt-and-braces guard
	// against other panics inside the stdlib CBC path (e.g. future
	// invariants we don't currently enforce). Removing either layer would
	// mean relying on the other — this way a regression in one is still
	// caught by the other. See JWE-005 in the v4 security review.
	// All pre-MAC structural failures return the exact same error value
	// as the post-MAC failure below. Distinguishing "malformed nonce",
	// "ciphertext too short", "ciphertext length not block-aligned", and
	// "MAC mismatch" at the caller would leak whether an attacker probe
	// is block-aligned vs cryptographically invalid — a structural-vs-MAC
	// oracle that composes with other leaks. Keep all four paths opaque.
	if len(nonce) != c.blockCipher.BlockSize() {
		return nil, errInvalidCiphertext
	}
	if len(ciphertext) < c.tlen {
		return nil, errInvalidCiphertext
	}

	tagOffset := len(ciphertext) - c.tlen
	if tagOffset%c.blockCipher.BlockSize() != 0 {
		return nil, errInvalidCiphertext
	}
	tag := ciphertext[tagOffset:]
	ciphertext = ciphertext[:tagOffset]

	expectedTag, err := c.ComputeAuthTag(data, nonce, ciphertext[:tagOffset])
	if err != nil {
		return nil, fmt.Errorf(`failed to compute auth tag: %w`, err)
	}

	cbc := cipher.NewCBCDecrypter(c.blockCipher, nonce)
	buf := pool.ByteSlice().GetCapacity(tagOffset)[:tagOffset]
	defer pool.ByteSlice().Put(buf)

	cbc.CryptBlocks(buf, ciphertext)

	toRemove, good := extractPadding(buf)
	cmp := subtle.ConstantTimeCompare(expectedTag, tag) & int(good)
	if cmp != 1 {
		return nil, errInvalidCiphertext
	}

	plaintext := buf[:len(buf)-toRemove]
	ret := ensureSize(dst, len(plaintext))
	out := ret[len(dst):]
	copy(out, plaintext)
	return ret, nil
}
