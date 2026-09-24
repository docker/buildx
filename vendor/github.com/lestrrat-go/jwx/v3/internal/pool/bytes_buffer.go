package pool

import "bytes"

var bytesBufferPool = New[*bytes.Buffer](allocBytesBuffer, freeBytesBuffer)

func allocBytesBuffer() *bytes.Buffer {
	return &bytes.Buffer{}
}

func freeBytesBuffer(b *bytes.Buffer) *bytes.Buffer {
	// Zero the backing array before returning to pool — the buffer
	// may hold private-key material, plaintext, or HMAC input.
	// b.Bytes() shares the internal slice (offset is always 0 in
	// our write-only usage); reslicing to cap reaches all residual bytes.
	if buf := b.Bytes(); cap(buf) > 0 {
		clear(buf[:cap(buf)])
	}
	b.Reset()
	return b
}

func BytesBuffer() *Pool[*bytes.Buffer] {
	return bytesBufferPool
}
