package ioset

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSingleForwarderEOF(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		err        error
	}{
		{"empty", "", io.EOF},
		{"data with EOF", "hello", io.EOF},
		{"wrapped EOF", "hello", errors.Wrap(io.EOF, "read")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewSingleForwarder()
			t.Cleanup(func() { require.NoError(t, f.Close()) })
			var output bytes.Buffer
			done := make(chan struct{})
			f.SetWriter(testWriteCloser{&output}, func() io.WriteCloser {
				close(done)
				return nil
			})
			f.SetReader(io.NopCloser(&finalRead{data: tc.data, err: tc.err}))
			select {
			case <-done:
				require.Equal(t, tc.data, output.String())
			case <-time.After(2 * time.Second):
				t.Fatal("EOF handler was not called")
			}
		})
	}
}

type finalRead struct {
	data string
	err  error
}

func (r *finalRead) Read(p []byte) (int, error) { return copy(p, r.data), r.err }

type testWriteCloser struct{ io.Writer }

func (testWriteCloser) Close() error { return nil }
