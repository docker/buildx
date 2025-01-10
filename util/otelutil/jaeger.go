package otelutil

import (
	"fmt"

	"github.com/docker/buildx/util/otelutil/jaeger"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

type JaegerData struct {
	Data []jaeger.Trace `json:"data"`
}

// JaegerData return Jaeger data compatible with ui import feature.
// https://github.com/jaegertracing/jaeger-ui/issues/381#issuecomment-494150826
func (s Spans) JaegerData() JaegerData {
	roSpans := s.ReadOnlySpans()

	// fetch default service.name from default resource for backup
	var defaultServiceName string
	defaultResource := resource.Default()
	if value, exists := defaultResource.Set().Value(attribute.Key("service.name")); exists {
		defaultServiceName = value.AsString()
	}

	data := jaeger.Trace{
		TraceID:   jaeger.TraceID(roSpans[0].SpanContext().TraceID().String()),
		Processes: make(map[jaeger.ProcessID]jaeger.Process),
		Spans:     []jaeger.Span{},
	}
	for i := range roSpans {
		ss := roSpans[i]
		pid := jaeger.ProcessID(fmt.Sprintf("p%d", i))
		data.Processes[pid] = jaeger.ResourceToProcess(ss.Resource(), defaultServiceName)
		span := jaeger.ConvertSpan(ss)
		span.Process = nil
		span.ProcessID = pid
		data.Spans = append(data.Spans, span)
	}

	return JaegerData{
		Data: []jaeger.Trace{data},
	}
}
