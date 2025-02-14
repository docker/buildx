package jaeger

import (
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	keyInstrumentationLibraryName    = "otel.library.name"
	keyInstrumentationLibraryVersion = "otel.library.version"
	keyError                         = "error"
	keySpanKind                      = "span.kind"
	keyStatusCode                    = "otel.status_code"
	keyStatusMessage                 = "otel.status_description"
	keyDroppedAttributeCount         = "otel.event.dropped_attributes_count"
	keyEventName                     = "event"
)

func ResourceToProcess(res *resource.Resource, defaultServiceName string) Process {
	var process Process
	var serviceName attribute.KeyValue
	if res != nil {
		for iter := res.Iter(); iter.Next(); {
			if iter.Attribute().Key == attribute.Key("service.name") {
				serviceName = iter.Attribute()
				// Don't convert service.name into tag.
				continue
			}
			if tag := keyValueToJaegerTag(iter.Attribute()); tag != nil {
				process.Tags = append(process.Tags, *tag)
			}
		}
	}

	// If no service.name is contained in a Span's Resource,
	// that field MUST be populated from the default Resource.
	if serviceName.Value.AsString() == "" {
		serviceName = attribute.Key("service.version").String(defaultServiceName)
	}
	process.ServiceName = serviceName.Value.AsString()

	return process
}

func ConvertSpan(ss tracesdk.ReadOnlySpan) Span {
	attr := ss.Attributes()
	tags := make([]KeyValue, 0, len(attr))
	for _, kv := range attr {
		tag := keyValueToJaegerTag(kv)
		if tag != nil {
			tags = append(tags, *tag)
		}
	}

	if is := ss.InstrumentationScope(); is.Name != "" {
		tags = append(tags, getStringTag(keyInstrumentationLibraryName, is.Name))
		if is.Version != "" {
			tags = append(tags, getStringTag(keyInstrumentationLibraryVersion, is.Version))
		}
	}

	if ss.SpanKind() != trace.SpanKindInternal {
		tags = append(tags,
			getStringTag(keySpanKind, ss.SpanKind().String()),
		)
	}

	if ss.Status().Code != codes.Unset {
		switch ss.Status().Code {
		case codes.Ok:
			tags = append(tags, getStringTag(keyStatusCode, "OK"))
		case codes.Error:
			tags = append(tags, getBoolTag(keyError, true))
			tags = append(tags, getStringTag(keyStatusCode, "ERROR"))
		}
		if ss.Status().Description != "" {
			tags = append(tags, getStringTag(keyStatusMessage, ss.Status().Description))
		}
	}

	var logs []Log
	for _, a := range ss.Events() {
		nTags := len(a.Attributes)
		if a.Name != "" {
			nTags++
		}
		if a.DroppedAttributeCount != 0 {
			nTags++
		}
		fields := make([]KeyValue, 0, nTags)
		if a.Name != "" {
			// If an event contains an attribute with the same key, it needs
			// to be given precedence and overwrite this.
			fields = append(fields, getStringTag(keyEventName, a.Name))
		}
		for _, kv := range a.Attributes {
			tag := keyValueToJaegerTag(kv)
			if tag != nil {
				fields = append(fields, *tag)
			}
		}
		if a.DroppedAttributeCount != 0 {
			fields = append(fields, getInt64Tag(keyDroppedAttributeCount, int64(a.DroppedAttributeCount)))
		}
		logs = append(logs, Log{
			Timestamp: timeAsEpochMicroseconds(a.Time),
			Fields:    fields,
		})
	}

	var refs []Reference
	for _, link := range ss.Links() {
		refs = append(refs, Reference{
			RefType: FollowsFrom,
			TraceID: TraceID(link.SpanContext.TraceID().String()),
			SpanID:  SpanID(link.SpanContext.SpanID().String()),
		})
	}
	refs = append(refs, Reference{
		RefType: ChildOf,
		TraceID: TraceID(ss.Parent().TraceID().String()),
		SpanID:  SpanID(ss.Parent().SpanID().String()),
	})

	return Span{
		TraceID:       TraceID(ss.SpanContext().TraceID().String()),
		SpanID:        SpanID(ss.SpanContext().SpanID().String()),
		Flags:         uint32(ss.SpanContext().TraceFlags()),
		OperationName: ss.Name(),
		References:    refs,
		StartTime:     timeAsEpochMicroseconds(ss.StartTime()),
		Duration:      durationAsMicroseconds(ss.EndTime().Sub(ss.StartTime())),
		Tags:          tags,
		Logs:          logs,
	}
}

func keyValueToJaegerTag(keyValue attribute.KeyValue) *KeyValue {
	var tag *KeyValue
	switch keyValue.Value.Type() {
	case attribute.STRING:
		s := keyValue.Value.AsString()
		tag = &KeyValue{
			Key:   string(keyValue.Key),
			Type:  StringType,
			Value: s,
		}
	case attribute.BOOL:
		b := keyValue.Value.AsBool()
		tag = &KeyValue{
			Key:   string(keyValue.Key),
			Type:  BoolType,
			Value: b,
		}
	case attribute.INT64:
		i := keyValue.Value.AsInt64()
		tag = &KeyValue{
			Key:   string(keyValue.Key),
			Type:  Int64Type,
			Value: i,
		}
	case attribute.FLOAT64:
		f := keyValue.Value.AsFloat64()
		tag = &KeyValue{
			Key:   string(keyValue.Key),
			Type:  Float64Type,
			Value: f,
		}
	case attribute.BOOLSLICE,
		attribute.INT64SLICE,
		attribute.FLOAT64SLICE,
		attribute.STRINGSLICE:
		data, _ := json.Marshal(keyValue.Value.AsInterface())
		a := (string)(data)
		tag = &KeyValue{
			Key:   string(keyValue.Key),
			Type:  StringType,
			Value: a,
		}
	}
	return tag
}

func getInt64Tag(k string, i int64) KeyValue {
	return KeyValue{
		Key:   k,
		Type:  Int64Type,
		Value: i,
	}
}

func getStringTag(k, s string) KeyValue {
	return KeyValue{
		Key:   k,
		Type:  StringType,
		Value: s,
	}
}

func getBoolTag(k string, b bool) KeyValue {
	return KeyValue{
		Key:   k,
		Type:  BoolType,
		Value: b,
	}
}

// timeAsEpochMicroseconds converts time.Time to microseconds since epoch,
// which is the format the StartTime field is stored in the Span.
func timeAsEpochMicroseconds(t time.Time) uint64 {
	return uint64(t.UnixNano() / 1000)
}

// durationAsMicroseconds converts time.Duration to microseconds,
// which is the format the Duration field is stored in the Span.
func durationAsMicroseconds(d time.Duration) uint64 {
	return uint64(d.Nanoseconds() / 1000)
}
