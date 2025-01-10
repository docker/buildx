package otelutil

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
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
