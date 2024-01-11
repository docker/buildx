package metric

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
				attribute.Stringer("service.instance.id", uuid.New()),
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
