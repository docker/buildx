package otelutil

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/docker/buildx/util/otelutil/jaeger"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
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

func TestJaegerAttributeValues(t *testing.T) {
	tests := []struct {
		name  string
		value attribute.Value
		want  string
	}{
		{
			name:  "empty",
			value: attribute.Value{},
			want:  `{"key":"key","type":"string","value":""}`,
		},
		{
			name:  "bytes",
			value: attribute.ByteSliceValue([]byte{0, 1, 255}),
			want:  `{"key":"key","type":"binary","value":"AAH/"}`,
		},
		{
			name:  "slice",
			value: attribute.SliceValue(attribute.Int64Value(9007199254740993), attribute.Value{}),
			want:  `{"key":"key","type":"string","value":"[9007199254740993,null]"}`,
		},
		{
			name:  "map",
			value: attribute.MapValue(attribute.KeyValue{Key: "nested", Value: attribute.SliceValue(attribute.BoolValue(true))}),
			want:  `{"key":"key","type":"string","value":"{\"nested\":[true]}"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := []attribute.KeyValue{{Key: "key", Value: tt.value}}
			spans := Spans{{
				SpanKind:   trace.SpanKindInternal,
				Attributes: attrs,
				Resource:   attrs,
				Events: []sdktrace.Event{{
					Attributes: attrs,
				}},
			}}
			data := spans.JaegerData().Data[0]
			span := data.Spans[0]
			for _, tags := range [][]jaeger.KeyValue{span.Tags, span.Logs[0].Fields, data.Processes[span.ProcessID].Tags} {
				require.Len(t, tags, 1)
				encoded, err := json.Marshal(tags[0])
				require.NoError(t, err)
				require.JSONEq(t, tt.want, string(encoded))
			}
		})
	}
}

func TestJaegerTraceFlags(t *testing.T) {
	for _, flags := range []trace.TraceFlags{0, trace.FlagsSampled, trace.FlagsRandom, trace.FlagsSampled | trace.FlagsRandom} {
		t.Run(flags.String(), func(t *testing.T) {
			span := Span{
				SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
					TraceFlags: flags,
				}),
			}
			got := jaeger.ConvertSpan(span.Snapshot())
			if flags.IsSampled() {
				require.Equal(t, uint32(1), got.Flags)
			} else {
				require.Zero(t, got.Flags)
			}
		})
	}
}
