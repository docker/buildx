package progress

import (
	"context"
	"strings"
	"time"

	sdkmetric "github.com/docker/buildx/otel/sdk/metric"
	"github.com/moby/buildkit/client"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type (
	buildStatus int
)

const (
	buildStatusComplete buildStatus = iota
	buildStatusCanceled
	buildStatusError
)

type RecordFunc func(ctx context.Context)

type metricWriter struct {
	Writer
	meter  metric.Meter
	opts   []metric.MeasurementOption
	status buildStatus
	start  time.Time
}

func Metrics(mp metric.MeterProvider, pw Writer, opts ...metric.MeasurementOption) (Writer, RecordFunc) {
	mw := &metricWriter{
		Writer: pw,
		meter:  sdkmetric.Meter(mp),
		opts:   opts,
		status: buildStatusComplete,
		start:  time.Now(),
	}
	return mw, mw.Record
}

func (mw *metricWriter) Write(ss *client.SolveStatus) {
	mw.write(ss)
	mw.Writer.Write(ss)
}

func (mw *metricWriter) write(ss *client.SolveStatus) {
	for _, v := range ss.Vertexes {
		if v.Error != "" {
			newBuildStatus := buildStatusError
			if strings.HasSuffix(v.Error, context.Canceled.Error()) {
				newBuildStatus = buildStatusCanceled
			}

			if mw.status < newBuildStatus {
				mw.status = newBuildStatus
			}
		}
	}
}

func (mw *metricWriter) Record(ctx context.Context) {
	mw.record(ctx)
}

func (mw *metricWriter) record(ctx context.Context) {
	buildDuration, _ := mw.meter.Int64Counter("build.duration",
		metric.WithDescription("Measures the total build duration."),
		metric.WithUnit("ms"))

	totalDur := time.Since(mw.start)
	opts := toAddOptions(mw.opts,
		metric.WithAttributes(mw.statusAttribute()),
	)
	buildDuration.Add(ctx, int64(totalDur/time.Millisecond), opts...)
}

func (mw *metricWriter) statusAttribute() attribute.KeyValue {
	status := "unknown"
	switch mw.status {
	case buildStatusComplete:
		status = "completed"
	case buildStatusCanceled:
		status = "canceled"
	case buildStatusError:
		status = "error"
	}
	return attribute.String("status", status)
}

func toAddOptions(opts []metric.MeasurementOption, extraOpts ...metric.AddOption) []metric.AddOption {
	newOpts := make([]metric.AddOption, 0, len(opts)+len(extraOpts))
	for _, opt := range opts {
		newOpts = append(newOpts, opt)
	}
	newOpts = append(newOpts, extraOpts...)
	return newOpts
}
