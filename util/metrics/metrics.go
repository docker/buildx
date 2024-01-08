package metrics

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/tracing/detect"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"golang.org/x/sync/errgroup"
)

const (
	otelConfigFieldName = "otel"
	shutdownTimeout     = 2 * time.Second
)

// ReportFunc is invoked to signal the metrics should be sent to the
// desired endpoint. It should be invoked on application shutdown.
type ReportFunc func()

// MeterProvider returns a MeterProvider suitable for CLI usage.
// The primary difference between this metric reader and a more typical
// usage is that metric reporting only happens once when ReportFunc
// is invoked.
func MeterProvider(cli command.Cli) (metric.MeterProvider, ReportFunc, error) {
	var exps []sdkmetric.Exporter

	if exp, err := dockerOtelExporter(cli); err != nil {
		return nil, nil, err
	} else if exp != nil {
		exps = append(exps, exp)
	}

	if exp, err := detectOtlpExporter(context.Background()); err != nil {
		return nil, nil, err
	} else if exp != nil {
		exps = append(exps, exp)
	}

	if len(exps) == 0 {
		// No exporters are configured so use a noop provider.
		return noop.NewMeterProvider(), func() {}, nil
	}

	// Use delta temporality because, since this is a CLI program, we can never
	// know the cumulative value.
	reader := sdkmetric.NewManualReader(
		sdkmetric.WithTemporalitySelector(deltaTemporality),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(detect.Resource()),
		sdkmetric.WithReader(reader),
	)
	return mp, reportFunc(reader, exps), nil
}

// reportFunc returns a ReportFunc for collecting ResourceMetrics and then
// exporting them to the configured Exporter.
func reportFunc(reader sdkmetric.Reader, exps []sdkmetric.Exporter) ReportFunc {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		var rm metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &rm); err != nil {
			// Error when collecting metrics. Do not send any.
			return
		}

		var eg errgroup.Group
		for _, exp := range exps {
			exp := exp
			eg.Go(func() error {
				_ = exp.Export(ctx, &rm)
				_ = exp.Shutdown(ctx)
				return nil
			})
		}

		// Can't report an error because we don't allow it to.
		_ = eg.Wait()
	}
}

// dockerOtelExporter reads the CLI metadata to determine an OTLP exporter
// endpoint for docker metrics to be sent.
//
// This location, configuration, and usage is hard-coded as part of
// sending usage statistics so this metric reporting is not meant to be
// user facing.
func dockerOtelExporter(cli command.Cli) (sdkmetric.Exporter, error) {
	endpoint, err := otelExporterOtlpEndpoint(cli)
	if endpoint == "" || err != nil {
		return nil, err
	}

	// Parse the endpoint. The docker config expects the endpoint to be
	// in the form of a URL to match the environment variable, but this
	// option doesn't correspond directly to WithEndpoint.
	//
	// We pretend we're the same as the environment reader.
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.Errorf("docker otel endpoint is invalid: %s", err)
	}

	var opts []otlpmetricgrpc.Option
	switch u.Scheme {
	case "unix":
		// Unix sockets are a bit weird. OTEL seems to imply they
		// can be used as an environment variable and are handled properly,
		// but they don't seem to be as the behavior of the environment variable
		// is to strip the scheme from the endpoint, but the underlying implementation
		// needs the scheme to use the correct resolver.
		//
		// We'll just handle this in a special way and add the unix:// back to the endpoint.
		opts = []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(fmt.Sprintf("unix://%s", path.Join(u.Host, u.Path))),
			otlpmetricgrpc.WithInsecure(),
		}
	case "http":
		opts = []otlpmetricgrpc.Option{
			// Omit the scheme when using http or https.
			otlpmetricgrpc.WithEndpoint(path.Join(u.Host, u.Path)),
			otlpmetricgrpc.WithInsecure(),
		}
	default:
		opts = []otlpmetricgrpc.Option{
			// Omit the scheme when using http or https.
			otlpmetricgrpc.WithEndpoint(path.Join(u.Host, u.Path)),
		}
	}

	// Hardcoded endpoint from the endpoint.
	exp, err := otlpmetricgrpc.New(context.Background(), opts...)
	if err != nil {
		return nil, err
	}
	return exp, nil
}

// otelExporterOtlpEndpoint retrieves the OTLP endpoint used for the docker reporter
// from the current context.
func otelExporterOtlpEndpoint(cli command.Cli) (string, error) {
	meta, err := cli.ContextStore().GetMetadata(cli.CurrentContext())
	if err != nil {
		return "", err
	}

	var otelCfg interface{}
	switch m := meta.Metadata.(type) {
	case command.DockerContext:
		otelCfg = m.AdditionalFields[otelConfigFieldName]
	case map[string]interface{}:
		otelCfg = m[otelConfigFieldName]
	}

	if otelCfg == nil {
		return "", nil
	}

	otelMap, ok := otelCfg.(map[string]interface{})
	if !ok {
		return "", errors.Errorf(
			"unexpected type for field %q: %T (expected: %T)",
			otelConfigFieldName,
			otelCfg,
			otelMap,
		)
	}

	// keys from https://opentelemetry.io/docs/concepts/sdk-configuration/otlp-exporter-configuration/
	endpoint, _ := otelMap["OTEL_EXPORTER_OTLP_ENDPOINT"].(string)
	return endpoint, nil
}

// deltaTemporality sets the Temporality of every instrument to delta.
func deltaTemporality(_ sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.DeltaTemporality
}
