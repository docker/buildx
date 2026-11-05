package bundle

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// countingProvider counts ReadAt calls per blob, each of which is one
// Content/Read RPC when the provider is a remote BuildKit content store.
type countingProvider struct {
	content.InfoReaderProvider
	reads map[digest.Digest]int
}

func newCountingProvider(p content.InfoReaderProvider) *countingProvider {
	return &countingProvider{InfoReaderProvider: p, reads: map[digest.Digest]int{}}
}

func (p *countingProvider) ReaderAt(ctx context.Context, desc ocispecs.Descriptor) (content.ReaderAt, error) {
	ra, err := p.InfoReaderProvider.ReaderAt(ctx, desc)
	if err != nil {
		return nil, err
	}
	return &countingReaderAt{ReaderAt: ra, count: func() { p.reads[desc.Digest]++ }}, nil
}

type countingReaderAt struct {
	content.ReaderAt
	count func()
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.count()
	return r.ReaderAt.ReadAt(p, off)
}

func TestSanitizeTraceReadsBlobOnce(t *testing.T) {
	ctx := context.TODO()

	dt, err := os.ReadFile("../../otelutil/fixtures/bktraces.json")
	require.NoError(t, err)
	dt = bytes.Replace(dt, []byte(`"Value": "grpc"`), []byte(`"Value": "git fetch token=hunter2"`), 1)

	desc := ocispecs.Descriptor{
		MediaType: "application/json",
		Digest:    digest.FromBytes(dt),
		Size:      int64(len(dt)),
	}
	buf := contentutil.NewBuffer()
	require.NoError(t, content.WriteBlob(ctx, buf, "trace", bytes.NewReader(dt), desc))

	p := newCountingProvider(buf)
	mp := contentutil.NewMultiProvider(p)
	out, err := sanitizeTrace(ctx, mp, desc)
	require.NoError(t, err)
	require.Equal(t, 1, p.reads[desc.Digest])

	sanitized, err := content.ReadBlob(ctx, mp, *out)
	require.NoError(t, err)
	require.Contains(t, string(sanitized), "token=xxxxx")
	require.NotContains(t, string(sanitized), "hunter2")
}
