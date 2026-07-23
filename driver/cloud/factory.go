package cloud

import (
	"context"
	"net/url"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/version"
	dockerclient "github.com/moby/moby/client"
	"github.com/pkg/errors"
)

const EndpointPrefix = "cloud://"

const (
	DriverName = "cloud"
	// CloudPullExportType is the type of export used for enabling
	// cloud pulls.
	CloudPullExportType = "cloud"
	AuthHost            = "https://auth.docker.io"
	ProxyAddress        = "tcp://build-cloud.docker.com:443"
	RegistryAddress     = "build-cloud.docker.com:443"
	HealthAddress       = "https://build-cloud.docker.com:443/v2/"

	// We want a low priority for the cloud driver for now.
	lowPriority      = 1000
	hubRegistryEntry = "https://index.docker.io/v1/"
	service          = "service=build.docker.com"
	// authorizationKey is the header for bearer token
	authorizationKey = "authorization"
	// buildxVersionKey is the header to identify the client version, eg: v0.5.1-desktop.1
	buildxVersionKey = "buildx-version"
	// userContextKey is the header to specify where the build is occurring, eg: local, ci, github, gitlab, circleci, travis, buildkite, jenkins
	userContextKey = "user-context"
	// userPlatformKey is the header to specify the platform which triggered the build, eg: linux/amd64, darwin/amd64
	userPlatformKey = "user-platform"
	// optKeyInternalAddress key to store the optional address for cloud endpoints
	// overrides specific internal.cloud.* addresses
	optKeyInternalAddress = "internal.address"
	// optKeyInternalAddress key to store the optional buildkit proxy address
	optKeyInternalProxyAddress = "internal.cloud.proxy.address"
	// optKeyInternalRegistryAddress key to store the optional cloud registry address for cloud-pull
	// defaults to `internal.address` on https
	optKeyInternalRegistryAddress = "internal.cloud.registry.address"
	// optKeyInternalHealthAddress key to store the optional cloud health check address
	// defaults to `internal.cloud.registry.address`/v2
	optKeyInternalHealthAddress = "internal.cloud.health.address"
)

var (
	// CouldPullExportRequiredAttributes is the list of required attributes for the Export entry
	// when doing a cloud pull.
	CouldPullExportRequiredAttributes = map[string]string{
		"attestation-inline": "false",
	}
)

func init() {
	driver.Register(&factory{})
}

type factory struct {
	headers []string
	once    sync.Once
}

var _ driver.DefaultBuilderNamer = (*factory)(nil)

func (*factory) Name() string {
	return DriverName
}

func (*factory) Usage() string {
	return DriverName
}

func (*factory) DefaultBuilderName(_ context.Context, endpoint string) (string, error) {
	return DriverName + "-" + strings.ReplaceAll(strings.ToLower(endpoint), "/", "-"), nil
}

func (*factory) Priority(ctx context.Context, endpoint string, api dockerclient.APIClient, _ map[string][]string) int {
	return lowPriority
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if len(cfg.Files) > 0 {
		return nil, errors.Errorf("setting config file is not supported for " + DriverName + " driver")
	}
	if len(cfg.BuildkitdFlags) > 0 {
		return nil, errors.Errorf("setting buildkit flags is not supported for " + DriverName + " driver")
	}
	f.prepareHeaders()
	d := &Driver{
		factory:         f,
		InitConfig:      cfg,
		proxyAddress:    ProxyAddress,
		registryAddress: RegistryAddress,
		healthAddress:   HealthAddress,
		builder:         strings.TrimPrefix(cfg.EndpointAddr, EndpointPrefix),
		headers:         f.headers,
	}

	authHost := AuthHost
	authEntry := hubRegistryEntry
	for k, v := range cfg.DriverOpts {
		switch k {
		case "default-load":
			parsed, err := strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
			d.defaultLoad = parsed
		case "builder", "org":
			// kept for backwards compat
			d.builder = v
		case optKeyInternalAddress:
			// Full override of cloud endpoints
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			d.proxyAddress = v
			// registry should not have a protocol, https is implicit
			d.registryAddress = strings.TrimPrefix(v, "tcp://")
			registryHTTPAddress := strings.Replace(v, "tcp", "https", 1)
			d.healthAddress = registryHTTPAddress + "/v2/"
		case optKeyInternalProxyAddress:
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			d.proxyAddress = v
		case optKeyInternalRegistryAddress:
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			d.registryAddress = v
		case optKeyInternalHealthAddress:
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			d.healthAddress = v
		case "internal.auth.host":
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			authHost = v
		case "internal.auth.entry":
			if err := verifyDomain(v); err != nil {
				return nil, err
			}
			authEntry = v
		case "internal.tls.insecure":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid value for %s", k)
			}
			d.tls.insecure = b
		case "internal.tls.servername":
			d.tls.serverName = v
		case "internal.tls.cacert":
			d.tls.caCert = v
		case "internal.hub.host":
			// used to call Cloud Builds API in group creation flow.
		default:
			return nil, errors.Errorf("invalid driver option %s for cloud driver", k)
		}
	}

	if d.builder == "" {
		return nil, errors.Errorf("builder name missing")
	}

	if len(strings.Split(d.builder, "/")) != 2 {
		return nil, errors.Errorf("builder should be in the format: <account>/<builder>")
	}

	d.tokenSource = newTokenSource(func(ctx context.Context) (string, error) {
		return login(ctx, authEntry, authHost, d.builder)
	}, time.Now)

	return d, nil
}

func (f *factory) prepareHeaders() {
	f.once.Do(func() {
		f.headers = []string{
			buildxVersionKey, getBuildxVersion(),
			userContextKey, getUserContext(os.Environ()),
			userPlatformKey, runtime.GOOS + "/" + runtime.GOARCH,
		}
	})
}

func (f *factory) AllowsInstances() bool {
	return true
}

// When called from desktop build backend, the version.Version is unknown. We try to get the version from the build info.
func getBuildxVersion() string {
	if version.Version != "v0.0.0+unknown" {
		return version.Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version.Version
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/docker/buildx" {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}

	return version.Version
}

func getUserContext(env []string) string {
	if !slices.Contains(env, "CI=true") {
		return "local"
	}
	switch {
	case slices.Contains(env, "GITHUB_ACTIONS=true"):
		return "github"
	case slices.Contains(env, "GITLAB_CI=true"):
		return "gitlab"
	case slices.Contains(env, "CIRCLECI=true"):
		return "circleci"
	case slices.Contains(env, "TRAVIS=true"):
		return "travis"
	case slices.Contains(env, "BUILDKITE=true"):
		return "buildkite"
	case slices.ContainsFunc(env, func(value string) bool { return strings.HasPrefix(value, "JENKINS_HOME=") }):
		return "jenkins"
	default:
		return "ci"
	}
}

func verifyDomain(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrap(err, "could not parse url")
	}

	valid := []string{
		"localhost",
		"docker.io",
		"docker.com",
	}
	host := u.Hostname()
	if !strings.ContainsRune(host, '.') {
		return nil
	}
	for _, valid := range valid {
		if host == valid {
			return nil
		}
		if strings.HasSuffix(host, "."+valid) {
			return nil
		}
	}
	return errors.Errorf("invalid domain %s", host)
}
