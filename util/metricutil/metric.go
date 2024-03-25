package metricutil

import (
	"context"

	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli/command"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Meter returns a Meter from the MetricProvider that indicates the measurement
// comes from buildx with the appropriate version.
func Meter(mp metric.MeterProvider) metric.Meter {
	return mp.Meter(version.Package,
		metric.WithInstrumentationVersion(version.Version))
}

// Shutdown invokes Shutdown on the MeterProvider and then reports any error to the OTEL handler.
func Shutdown(ctx context.Context, mp command.MeterProvider) {
	if err := mp.Shutdown(ctx); err != nil {
		otel.Handle(err)
	}
}
