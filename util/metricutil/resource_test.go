package metricutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
)

func TestResource(t *testing.T) {
	setErrorHandler(t)

	// Ensure resource creation doesn't result in an error.
	// This is because the schema urls for the various attributes need to be
	// the same, but it's really easy to import the wrong package when upgrading
	// otel to anew version and the buildx CLI swallows any visible errors.
	res := Resource()

	// Ensure an attribute is present.
	assert.True(t, res.Set().HasValue("telemetry.sdk.version"), "resource attribute missing")
}

func setErrorHandler(tb testing.TB) {
	tb.Helper()

	errorHandler := otel.GetErrorHandler()
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		tb.Errorf("otel error: %s", err)
	}))
	tb.Cleanup(func() {
		otel.SetErrorHandler(errorHandler)
	})
}
