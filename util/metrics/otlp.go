package metrics

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// detectOtlpExporter configures a metrics exporter based on environment variables.
// This is similar to the version of this in buildkit, but we need direct access
// to the exporter and the prometheus exporter doesn't work at all in a CLI context.
//
// There's some duplication here which I hope to remove when the detect package
// is refactored or extracted from buildkit so it can be utilized here.
//
// This version of the exporter is public facing in contrast to the
// docker otel collector.
func detectOtlpExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	set := os.Getenv("OTEL_METRICS_EXPORTER") == "otlp" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
	if !set {
		return nil, nil
	}

	proto := os.Getenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")
	if proto == "" {
		proto = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if proto == "" {
		proto = "grpc"
	}

	switch proto {
	case "grpc":
		return otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithTemporalitySelector(deltaTemporality),
		)
	case "http/protobuf":
		return otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithTemporalitySelector(deltaTemporality),
		)
	// case "http/json": // unsupported by library
	default:
		return nil, errors.Errorf("unsupported otlp protocol %v", proto)
	}
}
