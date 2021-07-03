package tracing

import (
	"context"
	"os"
	"strings"

	"github.com/moby/buildkit/util/tracing/detect"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TraceCurrentCommand(ctx context.Context, name string) (context.Context, func(error), error) {
	tp, err := detect.TracerProvider()
	if err != nil {
		return context.Background(), nil, err
	}
	ctx, span := tp.Tracer("").Start(ctx, name, trace.WithAttributes(
		attribute.String("command", strings.Join(os.Args, " ")),
	))

	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
		}
		span.End()

		detect.Shutdown(context.TODO())
	}, nil
}
