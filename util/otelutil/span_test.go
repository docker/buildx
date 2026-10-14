package otelutil

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	bktracesFixture = "./fixtures/bktraces.json"
	otlpFixture     = "./fixtures/otlp.json"
)

func TestParseSpanStubs(t *testing.T) {
	dt, err := os.ReadFile(bktracesFixture)
	require.NoError(t, err)

	spanStubs, err := ParseSpanStubs(bytes.NewReader(dt))
	require.NoError(t, err)
	require.Equal(t, 73, len(spanStubs))

	dtSpanStubs, err := json.MarshalIndent(spanStubs, "", "  ")
	require.NoError(t, err)
	dtotel, err := os.ReadFile(otlpFixture)
	require.NoError(t, err)
	require.Equal(t, string(bytes.TrimSpace(dtotel)), string(dtSpanStubs))

	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	require.NoError(t, err)
	require.NoError(t, exp.ExportSpans(context.Background(), spanStubs.ReadOnlySpans()))
}

func TestSpanAttributeRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		value attribute.Value
	}{
		{
			name:  "empty",
			value: attribute.Value{},
		},
		{
			name:  "bool",
			value: attribute.BoolValue(true),
		},
		{
			name:  "int64",
			value: attribute.Int64Value(math.MaxInt64),
		},
		{
			name:  "float64",
			value: attribute.Float64Value(1.25),
		},
		{
			name:  "string",
			value: attribute.StringValue("value"),
		},
		{
			name:  "bool slice",
			value: attribute.BoolSliceValue([]bool{true, false}),
		},
		{
			name:  "int64 slice",
			value: attribute.Int64SliceValue([]int64{math.MinInt64, math.MaxInt64}),
		},
		{
			name:  "float64 slice",
			value: attribute.Float64SliceValue([]float64{1.25, -2.5}),
		},
		{
			name:  "string slice",
			value: attribute.StringSliceValue([]string{"one", "two"}),
		},
		{
			name:  "bytes",
			value: attribute.ByteSliceValue([]byte{0, 1, 255}),
		},
		{
			name:  "slice",
			value: attribute.SliceValue(attribute.BoolValue(true), attribute.Int64Value(math.MaxInt64), attribute.Value{}),
		},
		{
			name:  "map",
			value: attribute.MapValue(attribute.String("name", "value"), attribute.Int64("number", math.MaxInt64)),
		},
		{
			name: "nested",
			value: attribute.SliceValue(attribute.MapValue(attribute.KeyValue{
				Key:   "nested",
				Value: attribute.SliceValue(attribute.ByteSliceValue([]byte{1}), attribute.Value{}),
			})),
		},
		{
			name:  "empty slice",
			value: attribute.SliceValue(),
		},
		{
			name:  "empty map",
			value: attribute.MapValue(),
		},
		{
			name:  "nil bool slice",
			value: attribute.BoolSliceValue(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := []attribute.KeyValue{{Key: "key", Value: tt.value}}
			scope := instrumentation.Scope{
				Name:       "test",
				Version:    "1.0",
				SchemaURL:  "https://example.com/schema",
				Attributes: attribute.NewSet(attrs...),
			}
			original := tracetest.SpanStub{
				Name:       "test",
				Attributes: attrs,
				Events: []sdktrace.Event{{
					Name:       "event",
					Time:       time.Unix(1, 0).UTC(),
					Attributes: attrs,
				}},
				Links: []sdktrace.Link{{
					Attributes: attrs,
				}},
				Resource:               resource.NewSchemaless(attrs...),
				InstrumentationScope:   scope,
				InstrumentationLibrary: scope,
			}
			var buf bytes.Buffer
			exporter, err := stdouttrace.New(stdouttrace.WithWriter(&buf))
			require.NoError(t, err)
			require.NoError(t, exporter.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{original.Snapshot()}))
			spans, err := ParseSpanStubs(&buf)
			require.NoError(t, err)
			require.Len(t, spans, 1)
			got := spans[0].Snapshot()
			assert.Equal(t, attrs, got.Attributes())
			assert.Equal(t, original.Events, got.Events())
			assert.Equal(t, original.Links, got.Links())
			assert.Equal(t, attrs, got.Resource().Attributes())
			assert.Equal(t, scope.Name, got.InstrumentationScope().Name)
			assert.Equal(t, scope.Version, got.InstrumentationScope().Version)
			assert.Equal(t, scope.SchemaURL, got.InstrumentationScope().SchemaURL)
			gotScope := got.InstrumentationScope()
			assert.Equal(t, attrs, gotScope.Attributes.ToSlice())

			// Bundle export marshals Span again, so verify the second round trip too.
			data, err := json.Marshal(&spans[0])
			require.NoError(t, err)
			var restored Span
			require.NoError(t, json.Unmarshal(data, &restored))
			assert.Equal(t, spans[0], restored)
		})
	}
}

func TestSpanContextRoundTrip(t *testing.T) {
	state, err := trace.ParseTraceState("vendor=value")
	require.NoError(t, err)
	for _, flags := range []trace.TraceFlags{0, trace.FlagsSampled, trace.FlagsRandom, trace.FlagsSampled | trace.FlagsRandom} {
		t.Run(flags.String(), func(t *testing.T) {
			sc := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{1},
				SpanID:     trace.SpanID{2},
				TraceFlags: flags,
				TraceState: state,
				Remote:     true,
			})
			original := tracetest.SpanStub{
				SpanContext: sc,
				Parent:      sc,
				Links: []sdktrace.Link{{
					SpanContext: sc,
				}},
			}
			data, err := json.Marshal(&original)
			require.NoError(t, err)
			var got Span
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, sc, got.SpanContext)
			assert.Equal(t, sc, got.Parent)
			assert.Equal(t, sc, got.Links[0].SpanContext)
		})
	}
}

func TestInstrumentationScopeCompatibility(t *testing.T) {
	for _, field := range []string{"InstrumentationScope", "InstrumentationLibrary", "both"} {
		t.Run(field, func(t *testing.T) {
			data, err := json.Marshal(&tracetest.SpanStub{})
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &fields))
			delete(fields, "InstrumentationScope")
			delete(fields, "InstrumentationLibrary")
			scopeJSON := json.RawMessage(`{"Name":"test","Version":"1","Attributes":[{"Key":"a","Value":{"Type":"STRING","Value":"b"}}]}`)
			if field == "both" {
				fields["InstrumentationScope"] = scopeJSON
				fields["InstrumentationLibrary"] = json.RawMessage(`{"Name":"legacy"}`)
			} else {
				fields[field] = scopeJSON
			}
			data, err = json.Marshal(fields)
			require.NoError(t, err)
			var got Span
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, "test", got.InstrumentationLibrary.Name)
			assert.Equal(t, []attribute.KeyValue{attribute.String("a", "b")}, got.InstrumentationLibrary.Attributes.ToSlice())
		})
	}
}

func TestInvalidAttributeJSON(t *testing.T) {
	for _, data := range []string{
		`{"Type":"BOOL","Value":null}`,
		`{"Type":"INT64","Value":null}`,
		`{"Type":"STRING","Value":null}`,
		`{"Type":"BOOL","Value":"true"}`,
		`{"Type":"INT64","Value":9223372036854775808}`,
		`{"Type":"INT64","Value":1.5}`,
		`{"Type":"FLOAT64","Value":"1.5"}`,
		`{"Type":"STRING","Value":1}`,
		`{"Type":"BOOLSLICE","Value":[1]}`,
		`{"Type":"INT64SLICE","Value":[1.5]}`,
		`{"Type":"FLOAT64SLICE","Value":["a"]}`,
		`{"Type":"STRINGSLICE","Value":["a",2]}`,
		`{"Type":"BYTESLICE","Value":"%%%"}`,
		`{"Type":"SLICE","Value":[{"Type":"UNKNOWN","Value":1}]}`,
		`{"Type":"MAP","Value":[{"Key":"a","Value":{"Type":"INT64","Value":"bad"}}]}`,
		`{"Type":"UNKNOWN","Value":1}`,
	} {
		t.Run(data, func(t *testing.T) {
			encoded, err := json.Marshal(&tracetest.SpanStub{})
			require.NoError(t, err)
			encoded = bytes.Replace(encoded, []byte(`"Attributes":null`), []byte(`"Attributes":[{"Key":"bad","Value":`+data+`}]`), 1)
			_, err = ParseSpanStubs(bytes.NewReader(encoded))
			require.Error(t, err)
		})
	}
}

func TestLegacyScopeAttributes(t *testing.T) {
	// Older bundles encoded Span by value, bypassing attribute.Set's pointer
	// marshaler and writing an empty object instead of an attribute array.
	data := []byte(`{
		"SpanContext": {
			"TraceID": "00000000000000000000000000000000",
			"SpanID": "0000000000000000",
			"TraceFlags": "00"
		},
		"Parent": {
			"TraceID": "00000000000000000000000000000000",
			"SpanID": "0000000000000000",
			"TraceFlags": "00"
		},
		"InstrumentationLibrary": {
			"Name": "legacy",
			"Attributes": {}
		}
	}`)
	var got Span
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, "legacy", got.InstrumentationLibrary.Name)
	require.Zero(t, got.InstrumentationLibrary.Attributes.Len())
}
