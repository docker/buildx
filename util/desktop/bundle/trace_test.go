package bundle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"testing"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/docker/buildx/util/otelutil"
	"github.com/moby/buildkit/util/contentutil"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestSanitizeTracePreservesSpanData(t *testing.T) {
	ctx := context.Background()
	state, err := trace.ParseTraceState("vendor=value")
	require.NoError(t, err)
	scope := instrumentation.Scope{
		Name:       "test",
		Attributes: attribute.NewSet(attribute.String("scope-key", "scope-value")),
	}
	original := tracetest.SpanStub{
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    trace.TraceID{1},
			SpanID:     trace.SpanID{2},
			TraceFlags: trace.FlagsSampled | trace.FlagsRandom,
			TraceState: state,
		}),
		Attributes: []attribute.KeyValue{
			attribute.String("command", "TOKEN=secret"),
			attribute.Int64("number", math.MaxInt64),
			attribute.BoolSlice("flags", []bool{true, false}),
		},
		InstrumentationScope: scope,
	}
	data, err := json.Marshal(&original)
	require.NoError(t, err)
	desc := ocispecs.Descriptor{
		Digest: digest.FromBytes(data),
		Size:   int64(len(data)),
	}
	buffer := contentutil.NewBuffer()
	require.NoError(t, content.WriteBlob(ctx, buffer, "trace", bytes.NewReader(data), desc))
	mp := contentutil.NewMultiProvider(buffer)
	result, err := sanitizeTrace(ctx, mp, desc)
	require.NoError(t, err)
	reader, err := mp.ReaderAt(ctx, *result)
	require.NoError(t, err)
	defer reader.Close()
	spans, err := otelutil.ParseSpanStubs(io.NewSectionReader(reader, 0, reader.Size()))
	require.NoError(t, err)
	require.Len(t, spans, 1)
	require.Equal(t, original.SpanContext, spans[0].SpanContext)
	require.Equal(t, []attribute.KeyValue{
		attribute.String("command", "TOKEN=xxxxx"),
		original.Attributes[1],
		original.Attributes[2],
	}, spans[0].Attributes)
	require.Equal(t, scope.Name, spans[0].InstrumentationLibrary.Name)
	require.Equal(t, scope.Attributes.ToSlice(), spans[0].InstrumentationLibrary.Attributes.ToSlice())
}

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
