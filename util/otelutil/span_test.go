package otelutil

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
)

// curl -s --unix-socket /tmp/docker-desktop-build-dev.sock http://localhost/blobs/default/default?digest=sha256:3103104e9fa908087bd47572da6ad9a5a7bf973608f736536d18d635a7da0140 -X GET > ./fixtures/bktraces.json
const bktracesFixture = "./fixtures/bktraces.json"

const otlpFixture = "./fixtures/otlp.json"

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
	require.Equal(t, string(dtotel), string(dtSpanStubs))

	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	require.NoError(t, err)
	require.NoError(t, exp.ExportSpans(context.Background(), spanStubs.ReadOnlySpans()))
}

func TestAsAttributeKeyValue(t *testing.T) {
	type args struct {
		Type  string
		value any
	}
	tests := []struct {
		name string
		args args
		want attribute.KeyValue
	}{
		{
			name: "string",
			args: args{
				Type:  attribute.STRING.String(),
				value: "value",
			},
			want: attribute.String("key", "value"),
		},
		{
			name: "int64 (int64)",
			args: args{
				Type:  attribute.INT64.String(),
				value: int64(1),
			},
			want: attribute.Int64("key", 1),
		},
		{
			name: "int64 (float64)",
			args: args{
				Type:  attribute.INT64.String(),
				value: float64(1.0),
			},
			want: attribute.Int64("key", 1),
		},
		{
			name: "bool",
			args: args{
				Type:  attribute.BOOL.String(),
				value: true,
			},
			want: attribute.Bool("key", true),
		},
		{
			name: "float64",
			args: args{
				Type:  attribute.FLOAT64.String(),
				value: float64(1.0),
			},
			want: attribute.Float64("key", 1.0),
		},
		{
			name: "float64slice",
			args: args{
				Type:  attribute.FLOAT64SLICE.String(),
				value: []float64{1.0, 2.0},
			},
			want: attribute.Float64Slice("key", []float64{1.0, 2.0}),
		},
		{
			name: "int64slice (int64)",
			args: args{
				Type:  attribute.INT64SLICE.String(),
				value: []int64{1, 2},
			},
			want: attribute.Int64Slice("key", []int64{1, 2}),
		},
		{
			name: "int64slice (float64)",
			args: args{
				Type:  attribute.INT64SLICE.String(),
				value: []float64{1.0, 2.0},
			},
			want: attribute.Int64Slice("key", []int64{1, 2}),
		},
		{
			name: "boolslice",
			args: args{
				Type:  attribute.BOOLSLICE.String(),
				value: []bool{true, false},
			},
			want: attribute.BoolSlice("key", []bool{true, false}),
		},
		{
			name: "stringslice (strings)",
			args: args{
				Type:  attribute.STRINGSLICE.String(),
				value: []string{"value1", "value2"},
			},
			want: attribute.StringSlice("key", []string{"value1", "value2"}),
		},
		{
			name: "stringslice (interface of string)",
			args: args{
				Type:  attribute.STRINGSLICE.String(),
				value: []any{"value1", "value2"},
			},
			want: attribute.StringSlice("key", []string{"value1", "value2"}),
		},
		{
			name: "stringslice (interface mixed)",
			args: args{
				Type:  attribute.STRINGSLICE.String(),
				value: []any{"value1", 2},
			},
			want: attribute.StringSlice("key", []string{"value1", "2"}),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			kv := keyValue{
				Key:   "key",
				Value: value{Type: tt.args.Type, Value: tt.args.value},
			}
			attr, err := kv.asAttributeKeyValue()
			require.NoError(t, err, "failed to convert key value to attribute key value")
			assert.Equal(t, tt.want, attr, "attribute key value mismatch")
		})
	}
}
