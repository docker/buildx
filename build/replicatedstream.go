package build

import (
	"bufio"
	"bytes"
	"io"
	"sync"
)

type SyncMultiReader struct {
	source  *bufio.Reader
	buffer  []byte
	static  []byte
	mu      sync.Mutex
	cond    *sync.Cond
	readers []*syncReader
	err     error
	offset  int
}

type syncReader struct {
	mr     *SyncMultiReader
	offset int
	closed bool
}

func NewSyncMultiReader(source io.Reader) *SyncMultiReader {
	mr := &SyncMultiReader{
		source: bufio.NewReader(source),
		buffer: make([]byte, 0, 32*1024),
	}
	mr.cond = sync.NewCond(&mr.mu)
	return mr
}

func (mr *SyncMultiReader) Peek(n int) ([]byte, error) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if mr.static != nil {
		return mr.static[min(n, len(mr.static)):], nil
	}

	return mr.source.Peek(n)
}

func (mr *SyncMultiReader) Reset(dt []byte) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	mr.static = dt
}

func (mr *SyncMultiReader) NewReadCloser() io.ReadCloser {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if mr.static != nil {
		return io.NopCloser(bytes.NewReader(mr.static))
	}

	reader := &syncReader{
		mr: mr,
	}
	mr.readers = append(mr.readers, reader)
	return reader
}

func (sr *syncReader) Read(p []byte) (int, error) {
	sr.mr.mu.Lock()
	defer sr.mr.mu.Unlock()

	return sr.read(p)
}

func (sr *syncReader) read(p []byte) (int, error) {
	end := sr.mr.offset + len(sr.mr.buffer)

loop0:
	for {
		if sr.closed {
			return 0, io.EOF
		}

		end := sr.mr.offset + len(sr.mr.buffer)

		if sr.mr.err != nil && sr.offset == end {
			return 0, sr.mr.err
		}

		start := sr.offset - sr.mr.offset

		dt := sr.mr.buffer[start:]

		if len(dt) > 0 {
			n := copy(p, dt)
			sr.offset += n
			sr.mr.cond.Broadcast()
			return n, nil
		}

		// check for readers that have not caught up
		hasOpen := false
		for _, r := range sr.mr.readers {
			if !r.closed {
				hasOpen = true
			} else {
				continue
			}
			if r.offset < end {
				sr.mr.cond.Wait()
				continue loop0
			}
		}

		if !hasOpen {
			return 0, io.EOF
		}
		break
	}

	last := sr.mr.offset + len(sr.mr.buffer)
	// another reader has already updated the buffer
	if last > end || sr.mr.err != nil {
		return sr.read(p)
	}

	sr.mr.offset += len(sr.mr.buffer)

	sr.mr.buffer = sr.mr.buffer[:cap(sr.mr.buffer)]
	n, err := sr.mr.source.Read(sr.mr.buffer)
	if n >= 0 {
		sr.mr.buffer = sr.mr.buffer[:n]
	} else {
		sr.mr.buffer = sr.mr.buffer[:0]
	}

	sr.mr.cond.Broadcast()

	if err != nil {
		sr.mr.err = err
		return 0, err
	}

	nn := copy(p, sr.mr.buffer)
	sr.offset += nn

	return nn, nil
}

func (sr *syncReader) Close() error {
	sr.mr.mu.Lock()
	defer sr.mr.mu.Unlock()

	if sr.closed {
		return nil
	}

	sr.closed = true

	sr.mr.cond.Broadcast()

	return nil
}
