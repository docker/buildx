package cloud

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/progress"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	sessionexporter "github.com/moby/buildkit/session/exporter"
	"github.com/moby/buildkit/session/exporter/exporterprovider"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/api/types/system"
	dockerclient "github.com/moby/moby/client"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	grpcgzip "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

func init() {
	if err := grpcgzip.SetLevel(gzip.BestCompression); err != nil {
		panic(err)
	}
}

type Driver struct {
	factory driver.Factory
	driver.InitConfig

	defaultLoad     bool
	builder         string
	tokenSource     tokenSource
	proxyAddress    string
	registryAddress string
	healthAddress   string
	tls             tlsOpts

	headers []string
}

var _ interface {
	driver.Driver
	driver.BuildPreparer
} = (*Driver)(nil)

// tlsOpts allows overriding the default TLS configuration for communicating
// with the registry. This should only be used for development and testing.
type tlsOpts struct {
	insecure   bool
	serverName string
	caCert     string
}

func (d *Driver) Dial(ctx context.Context) (net.Conn, error) {
	return nil, errors.New("cloud driver does not support dialing")
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return nil
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	// hit the /v2 to check if the registry is up
	url := d.healthAddress
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := d.tlsConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create TLS config: %v", err)
	}
	client := newClient(&http.Transport{
		TLSClientConfig: tlsConfig,
	})

	resp, err := client.Do(req)
	if resp != nil {
		defer drainResponse(resp)
	}
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return &driver.Info{
			Status: driver.Inactive,
		}, nil
	}

	return &driver.Info{
		Status: driver.Running,
	}, nil
}

func (d *Driver) Version(ctx context.Context) (string, error) {
	return "", nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	return nil
}

func (d *Driver) Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error) {
	if !d.tls.insecure {
		if d.tls.caCert != "" {
			opts = append(opts, client.WithServerConfig(d.tls.serverName, d.tls.caCert))
		} else {
			opts = append(opts, client.WithServerConfigSystem(d.tls.serverName))
		}
	}

	kp := keepalive.ClientParameters{Time: 10 * time.Second}
	opts = append(opts, client.WithGRPCDialOption(grpc.WithKeepaliveParams(kp)))

	i := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		token, err := d.tokenSource(ctx)
		if err != nil {
			return errors.Wrap(err, "fetching auth token")
		}
		headers := append(d.headers, authorizationKey, "Bearer "+token)
		ctx = metadata.AppendToOutgoingContext(ctx, headers...)
		opts = append([]grpc.CallOption{grpc.UseCompressor(grpcgzip.Name)}, opts...)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	i2 := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		token, err := d.tokenSource(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "fetching auth token")
		}
		headers := append(d.headers, authorizationKey, "Bearer "+token)
		ctx = metadata.AppendToOutgoingContext(ctx, headers...)
		opts = append([]grpc.CallOption{grpc.UseCompressor(grpcgzip.Name)}, opts...)
		return streamer(ctx, desc, cc, method, opts...)
	}
	opts = append(opts,
		[]client.ClientOpt{
			client.WithGRPCDialOption(grpc.WithChainUnaryInterceptor(i)),
			client.WithGRPCDialOption(grpc.WithChainStreamInterceptor(i2)),
		}...,
	)
	c, err := client.New(ctx, d.proxyAddress, opts...)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d *Driver) Features(_ context.Context) map[driver.Feature]bool {
	return map[driver.Feature]bool{
		driver.OCIExporter:    true,
		driver.DockerExporter: false,
		driver.CacheExport:    true,
		driver.MultiPlatform:  true,
		driver.DirectPush:     true,
		driver.DefaultLoad:    d.defaultLoad,
	}
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) IsMobyDriver() bool {
	return false
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}

func (d *Driver) HostGatewayIP(ctx context.Context) (net.IP, error) {
	return nil, errors.New("host-gateway is not supported by the cloud driver")
}

func (d *Driver) cloudPull(ctx context.Context, imageDescriptor string, platform string, duc *dockerutil.Client, dockerContext string, tags []string, l progress.SubLogger) error {
	if imageDescriptor == "" {
		return errors.Errorf("missing exporter response %q", exptypes.ExporterImageDescriptorKey)
	}
	decodedDescriptor, err := base64.StdEncoding.DecodeString(imageDescriptor)
	if err != nil {
		return err
	}
	var desc specs.Descriptor
	if err := json.Unmarshal(decodedDescriptor, &desc); err != nil {
		return errors.Wrapf(err, "unmarshal descriptor %s", decodedDescriptor)
	}
	sha := desc.Digest.String()

	dc, err := duc.API(dockerContext)
	if err != nil {
		return err
	}

	token, err := d.tokenSource(ctx)
	if err != nil {
		return errors.Wrap(err, "fetching auth token")
	}

	hydroRegistryAddr := d.registryAddress

	// Reuse the same token used for the build
	authConfig := registry.AuthConfig{
		RegistryToken: token,
		ServerAddress: hydroRegistryAddr,
	}
	authJSON, err := json.Marshal(authConfig)
	if err != nil {
		return err
	}
	base64Auth := base64.URLEncoding.EncodeToString(authJSON)
	var pullPlatforms []specs.Platform
	if platform != "" {
		targetPlatform, err := platforms.Parse(platform)
		if err != nil {
			return errors.Wrapf(err, "parsing platform %s", platform)
		}
		pullPlatforms = []specs.Platform{platforms.Normalize(targetPlatform)}
	}
	pullOpts := dockerclient.ImagePullOptions{
		RegistryAuth: base64Auth,
		Platforms:    pullPlatforms,
	}

	// Use builder name as tag (will be replaced with user tags after the pull)
	ref := fmt.Sprintf("%s/%s@%s", hydroRegistryAddr, d.builder, sha)

	pullResp, err := dc.ImagePull(ctx, ref, pullOpts)
	if err != nil {
		return err
	}
	defer pullResp.Close()

	if err := printMagicPull(pullResp, l); err != nil {
		return err
	}

	// Tag image with user tags and remove the builder name tag
	for _, tag := range tags {
		opts := dockerclient.ImageTagOptions{
			Source: ref,
			Target: tag,
		}
		if _, err = dc.ImageTag(ctx, opts); err != nil {
			return err
		}
	}

	// TODO: support case where no tags are provided
	removeOpts := dockerclient.ImageRemoveOptions{PruneChildren: false}
	_, err = dc.ImageRemove(ctx, ref, removeOpts)
	if err != nil {
		logrus.Debugf("failed to remove temp tag %s: %v", ref, err)
	}

	return nil
}

func (d *Driver) PrepareBuild(ctx context.Context, opt *driver.PrepareBuildOptions) error {
	if opt.NoOutput {
		return nil
	}
	if len(opt.Tags) == 0 {
		return nil
	}

	exports, requests, dockerContext := resolveCloudPullExporters(opt.SolveOpt.Exports, opt.Tags, opt.BuildInfoAttrs, opt.BuildInfoAttrsSet, opt.NoDefaultOCIArtifact)
	if len(requests) == 0 {
		return nil
	}

	if opt.MultiDriver || len(opt.Platforms) > 1 {
		return nil
	}

	supportsRegistryTokenPull, err := daemonSupportsRegistryTokenPull(ctx, opt.Docker, dockerContext)
	if err != nil {
		logrus.Warnf("failed to detect registry pull token support in daemon: %v", err)
		return nil
	}
	if !supportsRegistryTokenPull {
		return nil
	}

	opt.SolveOpt.Exports = exports
	opt.SolveOpt.Session = append(opt.SolveOpt.Session, d.newCloudPullSessionExporter(*opt, requests, dockerContext))
	opt.SolveOpt.EnableSessionExporter = true
	return nil
}

func (d *Driver) newCloudPullSessionExporter(opt driver.PrepareBuildOptions, requests []*sessionexporter.ExporterRequest, dockerContext string) *exporterprovider.Exporter {
	targetPlatform := d.cloudPullPlatform(opt)
	tags := slices.Clone(opt.Tags)
	return exporterprovider.New(func(context.Context, map[string][]byte, []string) ([]*sessionexporter.ExporterRequest, error) {
		return requests, nil
	}, exporterprovider.WithFinalizeCallback(func(ctx context.Context, exporterResponse map[string]string) error {
		imgDescriptor := exporterResponse[exptypes.ExporterImageDescriptorKey]
		if imgDescriptor == "" {
			return errors.Errorf("missing exporter response %q", exptypes.ExporterImageDescriptorKey)
		}
		pw := progress.ResetTime(opt.Progress)
		return progress.Wrap("cloud pull", pw.Write, func(l progress.SubLogger) error {
			if err := d.cloudPull(ctx, imgDescriptor, targetPlatform, opt.Docker, dockerContext, tags, l); err != nil {
				return errors.Wrap(err, "pulling image from cloud")
			}
			return nil
		})
	}))
}

func (d *Driver) cloudPullPlatform(opt driver.PrepareBuildOptions) string {
	targetPlatforms := opt.Platforms
	if len(targetPlatforms) == 0 {
		targetPlatforms = d.Config().Platforms
	}
	if len(targetPlatforms) != 1 {
		return ""
	}
	return platforms.Format(targetPlatforms[0])
}

func resolveCloudPullExporters(exports []client.ExportEntry, tags []string, buildInfoAttrs string, buildInfoAttrsSet bool, noDefaultOCIArtifact bool) ([]client.ExportEntry, []*sessionexporter.ExporterRequest, string) {
	exporterAttrs := func(attrs map[string]string) map[string]string {
		attrs = maps.Clone(attrs)
		if attrs == nil {
			attrs = map[string]string{}
		}
		attrs["attestation-inline"] = "false"
		if attrs["name"] == "" {
			attrs["name"] = strings.Join(tags, ",")
		}
		if buildInfoAttrsSet {
			attrs["buildinfo-attrs"] = buildInfoAttrs
		}
		if noDefaultOCIArtifact {
			if _, ok := attrs[string(exptypes.OptKeyOCIArtifact)]; !ok {
				attrs[string(exptypes.OptKeyOCIArtifact)] = "false"
			}
		}
		return attrs
	}

	if len(exports) == 0 {
		return nil, []*sessionexporter.ExporterRequest{{
			Type:  "image",
			Attrs: exporterAttrs(nil),
		}}, ""
	}

	var remaining []client.ExportEntry
	var requests []*sessionexporter.ExporterRequest
	var dockerContext string
	var hasDockerContext bool
	for _, export := range exports {
		push, _ := strconv.ParseBool(export.Attrs["push"])
		if export.Type != "docker" && (export.Type != "image" || push) {
			remaining = append(remaining, export)
			continue
		}
		if export.Output != nil || export.OutputDir != "" {
			remaining = append(remaining, export)
			continue
		}
		dctx := export.Attrs["context"]
		if !hasDockerContext {
			dockerContext = dctx
			hasDockerContext = true
		} else if dockerContext != dctx {
			return exports, nil, ""
		}
		request := &sessionexporter.ExporterRequest{
			Type:  "image",
			Attrs: exporterAttrs(export.Attrs),
		}
		if len(requests) > 0 {
			// FinalizeExport receives one response map, so cloud pull only
			// supports one distinct converted exporter request.
			if requests[0].Type == request.Type && maps.Equal(requests[0].Attrs, request.Attrs) {
				continue
			}
			return exports, nil, ""
		}
		requests = append(requests, request)
	}
	return remaining, requests, dockerContext
}

// tlsConfig returns a TLS configuration for communicating with the registry,
// based on d.tls. Note that this is only intended for development and testing.
func (d *Driver) tlsConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: d.tls.insecure,
	}
	if d.tls.caCert != "" {
		// Prefer the system cert pool if available, but fallback
		// to an empty pool
		rootCAs, _ := x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		ca, err := os.ReadFile(d.tls.caCert)
		if err != nil {
			return nil, errors.Wrapf(err, "could not read CA certificate %s: %v", d.tls.caCert, err)
		}
		if ok := rootCAs.AppendCertsFromPEM(ca); !ok {
			return nil, errors.Errorf("failed to add CA certificate %s to root CAs", d.tls.caCert)
		}
		tlsConfig.RootCAs = rootCAs
	}
	return tlsConfig, nil
}

func printMagicPull(rc io.Reader, l progress.SubLogger) (err error) {
	started := map[string]client.VertexStatus{}

	defer func() {
		if err != nil {
			return
		}
		for _, st := range started {
			if st.Completed == nil {
				now := time.Now()
				st.Completed = &now
				l.SetStatus(&st)
			}
		}
	}()

	dec := json.NewDecoder(rc)

	var (
		parsedError error
		jm          jsonstream.Message
	)

	for {
		if err = dec.Decode(&jm); err != nil {
			if parsedError != nil {
				err = parsedError
			} else if err == io.EOF {
				err = nil
			}
			return
		}

		if jm.Error != nil {
			parsedError = jm.Error
		}

		if jm.ID == "" {
			continue
		}

		// handle temporary fake registry tags
		if strings.ContainsRune(jm.ID, '@') {
			continue
		}
		if strings.ContainsRune(jm.Status, ':') {
			continue
		}

		id := "pulling layer " + jm.ID
		st, ok := started[id]
		if !ok {
			if jm.Progress != nil || strings.HasPrefix(jm.Status, "Pulling") || strings.HasPrefix(jm.Status, "Already exists") {
				now := time.Now()
				st = client.VertexStatus{
					ID:      id,
					Started: &now,
				}
			} else {
				continue
			}
		}
		st.Timestamp = time.Now()
		if jm.Progress != nil && jm.Status == "Downloading" {
			st.Current = jm.Progress.Current
			st.Total = jm.Progress.Total
		}
		if jm.Error != nil {
			now := time.Now()
			st.Completed = &now
		}

		if jm.Status == "Pull complete" || jm.Status == "Already exists" {
			now := time.Now()
			st.Completed = &now
			st.Current = st.Total
		}
		started[id] = st
		l.SetStatus(&st)
	}
}

func daemonSupportsRegistryTokenPull(ctx context.Context, duc *dockerutil.Client, dockerContext string) (bool, error) {
	dc, err := duc.API(dockerContext)
	if err != nil {
		return false, err
	}

	resp, err := dc.Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return false, err
	}
	return daemonInfoSupportsRegistryTokenPull(resp.Info)
}

func daemonInfoSupportsRegistryTokenPull(resp system.Info) (bool, error) {
	for _, status := range resp.DriverStatus {
		if len(status) == 2 && status[1] == "io.containerd.snapshotter.v1" {
			// If we're using the containerd snapshotter, we need
			// to check for https://github.com/moby/moby/pull/46475.
			//
			// Simplest way:
			// - if we're using docker desktop, assume we have a compatible version
			// - otherwise, check if the daemon is on 25.0.0 or newer
			constraint, err := semver.NewConstraint(">= 25.0.0")
			if err != nil {
				return false, err
			}

			if resp.OperatingSystem == "Docker Desktop" {
				return true, nil
			}

			version, err := semver.NewVersion(resp.ServerVersion)
			if err != nil {
				return false, err
			}

			return constraint.Check(version), nil
		}
	}

	return true, nil
}
