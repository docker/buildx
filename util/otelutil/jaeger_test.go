package otelutil

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/docker/buildx/util/otelutil/jaeger"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const jaegerFixture = "./fixtures/jaeger.json"

func TestJaegerData(t *testing.T) {
	dt, err := os.ReadFile(bktracesFixture)
	require.NoError(t, err)

	spanStubs, err := ParseSpanStubs(bytes.NewReader(dt))
	require.NoError(t, err)

	trace := spanStubs.JaegerData()
	dtJaegerTrace, err := json.MarshalIndent(trace, "", "  ")
	require.NoError(t, err)
	dtJaeger, err := os.ReadFile(jaegerFixture)
	require.NoError(t, err)
	require.Equal(t, string(dtJaeger), string(dtJaegerTrace))
}

func TestJaegerDataEmpty(t *testing.T) {
	for _, input := range []string{"", " \n\t"} {
		t.Run(input, func(t *testing.T) {
			spans, err := ParseSpanStubs(strings.NewReader(input))
			require.NoError(t, err)
			require.Empty(t, spans.JaegerData().Data)
		})
	}
}

func TestJaegerReferences(t *testing.T) {
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{1},
	})
	parent := spanContext.WithSpanID(trace.SpanID{2})
	linked := spanContext.WithSpanID(trace.SpanID{3})
	parentRef := jaeger.Reference{
		RefType: jaeger.ChildOf,
		TraceID: jaeger.TraceID(parent.TraceID().String()),
		SpanID:  jaeger.SpanID(parent.SpanID().String()),
	}
	linkRef := jaeger.Reference{
		RefType: jaeger.FollowsFrom,
		TraceID: jaeger.TraceID(linked.TraceID().String()),
		SpanID:  jaeger.SpanID(linked.SpanID().String()),
	}
	tests := []struct {
		name   string
		parent trace.SpanContext
		links  []sdktrace.Link
		want   []jaeger.Reference
	}{
		{
			name: "root",
		},
		{
			name:   "child",
			parent: parent,
			want:   []jaeger.Reference{parentRef},
		},
		{
			name:  "root with link",
			links: []sdktrace.Link{{SpanContext: linked}},
			want:  []jaeger.Reference{linkRef},
		},
		{
			name:   "child with link",
			parent: parent,
			links:  []sdktrace.Link{{SpanContext: linked}},
			want:   []jaeger.Reference{linkRef, parentRef},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := Spans{{
				SpanContext: spanContext,
				Parent:      tt.parent,
				Links:       tt.links,
			}}
			data := spans.JaegerData()
			require.Len(t, data.Data, 1)
			require.Len(t, data.Data[0].Spans, 1)
			require.Equal(t, tt.want, data.Data[0].Spans[0].References)
		})
	}
}
