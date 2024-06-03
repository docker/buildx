package tracing

import (
	"context"
	"os"
	"strings"

	"github.com/moby/buildkit/util/tracing/delegated"
	"github.com/moby/buildkit/util/tracing/detect"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TraceCurrentCommand(ctx context.Context, name string) (context.Context, func(error), error) {
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(detect.Resource()),
		sdktrace.WithBatcher(delegated.DefaultExporter),
	}
	if exp, err := detect.NewSpanExporter(ctx); err != nil {
		otel.Handle(err)
	} else if !detect.IsNoneSpanExporter(exp) {
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	ctx, span := tp.Tracer("").Start(ctx, name, trace.WithAttributes(
		attribute.String("command", strings.Join(os.Args, " ")),
	))

	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
		}
		span.End()

		_ = tp.Shutdown(context.TODO())
	}, nil
}
