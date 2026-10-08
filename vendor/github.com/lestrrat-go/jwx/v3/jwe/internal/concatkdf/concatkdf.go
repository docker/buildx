package concatkdf

import (
	"crypto"
	"encoding/binary"
	"fmt"
)

type KDF struct {
	buf       []byte
	otherinfo []byte
	z         []byte
	hash      crypto.Hash
}

func New(hash crypto.Hash, alg, Z, apu, apv, pubinfo, privinfo []byte) *KDF {
	// Write length-prefixed fields directly into a single buffer,
	// avoiding intermediate allocations from ndata().
	totalSize := (4 + len(alg)) + (4 + len(apu)) + (4 + len(apv)) + len(pubinfo) + len(privinfo)
	concat := make([]byte, totalSize)

	n := 0
	binary.BigEndian.PutUint32(concat[n:], uint32(len(alg)))
	n += 4
	n += copy(concat[n:], alg)

	binary.BigEndian.PutUint32(concat[n:], uint32(len(apu)))
	n += 4
	n += copy(concat[n:], apu)

	binary.BigEndian.PutUint32(concat[n:], uint32(len(apv)))
	n += 4
	n += copy(concat[n:], apv)

	n += copy(concat[n:], pubinfo)
	copy(concat[n:], privinfo)

	return &KDF{
		hash:      hash,
		otherinfo: concat,
		z:         Z,
	}
}

func (k *KDF) Read(out []byte) (int, error) {
	var round uint32 = 1
	h := k.hash.New()
	var roundBuf [4]byte

	for len(out) > len(k.buf) {
		h.Reset()

		binary.BigEndian.PutUint32(roundBuf[:], round)
		if _, err := h.Write(roundBuf[:]); err != nil {
			return 0, fmt.Errorf(`failed to write round using kdf: %w`, err)
		}
		if _, err := h.Write(k.z); err != nil {
			return 0, fmt.Errorf(`failed to write z using kdf: %w`, err)
		}
		if _, err := h.Write(k.otherinfo); err != nil {
			return 0, fmt.Errorf(`failed to write other info using kdf: %w`, err)
		}

		k.buf = h.Sum(k.buf)
		round++
	}

	n := copy(out, k.buf[:len(out)])
	k.buf = k.buf[len(out):]
	return n, nil
}
