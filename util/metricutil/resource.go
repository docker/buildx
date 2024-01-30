package metricutil

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

var (
	res     *resource.Resource
	resOnce sync.Once
)

// Resource retrieves the OTEL resource for the buildx CLI.
func Resource() *resource.Resource {
	resOnce.Do(func() {
		var err error
		res, err = resource.New(context.Background(),
			resource.WithDetectors(serviceNameDetector{}),
			resource.WithAttributes(
				// Use a unique instance id so OTEL knows that each invocation
				// of the CLI is its own instance. Without this, downstream
				// OTEL processors may think the same process is restarting
				// continuously and reset the metric counters.
				semconv.ServiceInstanceID(uuid.New().String()),
			),
			resource.WithFromEnv(),
			resource.WithTelemetrySDK(),
		)
		if err != nil {
			otel.Handle(err)
		}
	})
	return res
}

type serviceNameDetector struct{}

func (serviceNameDetector) Detect(ctx context.Context) (*resource.Resource, error) {
	return resource.StringDetector(
		semconv.SchemaURL,
		semconv.ServiceNameKey,
		func() (string, error) {
			return filepath.Base(os.Args[0]), nil
		},
	).Detect(ctx)
}
