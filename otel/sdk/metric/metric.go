package metric

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli/command"
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
	reportTimeout       = 2 * time.Second
)

// MeterProvider holds a MeterProvider for metric generation and the configured
// exporters for reporting metrics from the CLI.
type MeterProvider struct {
	metric.MeterProvider
	reader    *sdkmetric.ManualReader
	exporters []sdkmetric.Exporter
}

// NewMeterProvider configures a MeterProvider from the CLI context.
func NewMeterProvider(ctx context.Context, cli command.Cli) (*MeterProvider, error) {
	var exps []sdkmetric.Exporter

	// Only metric exporters if the experimental flag is set.
	if confutil.IsExperimental() {
		if exp, err := dockerOtelExporter(cli); err != nil {
			return nil, err
		} else if exp != nil {
			exps = append(exps, exp)
		}

		if exp, err := detectOtlpExporter(ctx); err != nil {
			return nil, err
		} else if exp != nil {
			exps = append(exps, exp)
		}
	}

	if len(exps) == 0 {
		// No exporters are configured so use a noop provider.
		return &MeterProvider{
			MeterProvider: noop.NewMeterProvider(),
		}, nil
	}

	reader := sdkmetric.NewManualReader(
		sdkmetric.WithTemporalitySelector(deltaTemporality),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(Resource()),
		sdkmetric.WithReader(reader),
	)
	return &MeterProvider{
		MeterProvider: mp,
		reader:        reader,
		exporters:     exps,
	}, nil
}

// Report exports metrics to the configured exporter. This should be done before the CLI
// exits.
func (m *MeterProvider) Report(ctx context.Context) {
	if m.reader == nil {
		// Not configured.
		return
	}

	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()

	var rm metricdata.ResourceMetrics
	if err := m.reader.Collect(ctx, &rm); err != nil {
		// Error when collecting metrics. Do not send any.
		return
	}

	var eg errgroup.Group
	for _, exp := range m.exporters {
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

// Meter returns a Meter from the MetricProvider that indicates the measurement
// comes from buildx with the appropriate version.
func Meter(mp metric.MeterProvider) metric.Meter {
	return mp.Meter(version.Package,
		metric.WithInstrumentationVersion(version.Version))
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
//
// This isn't really needed since we create a unique resource on each invocation,
// but it can help with cardinality concerns for downstream processors since they can
// perform aggregation for a time interval and then discard the data once that time
// period has passed. Cumulative temporality would imply to the downstream processor
// that they might receive a successive point and they may unnecessarily keep state
// they really shouldn't.
func deltaTemporality(_ sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.DeltaTemporality
}
