package pool

var byteSlicePool = SlicePool[byte]{
	pool: New[[]byte](allocByteSlice, freeByteSlice),
}

func allocByteSlice() []byte {
	return make([]byte, 0, 64) // Default capacity of 64 bytes
}

func freeByteSlice(b []byte) []byte {
	// Defensive: scrub the entire backing array, not just b[:len(b)]. No
	// current caller is known to reslice past len(b) and observe stale
	// bytes, but a defer Put(buf) that captures buf at len=0 (before a
	// subsequent buf = buf[:n]) would otherwise leave plaintext resident
	// in the pool's backing storage.
	b = b[:cap(b)]
	clear(b)
	return b[:0]
}

func ByteSlice() SlicePool[byte] {
	return byteSlicePool
}
