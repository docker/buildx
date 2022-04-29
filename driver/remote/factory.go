package remote

import (
	"context"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/docker/buildx/driver"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
)

const prioritySupported = 20
const priorityUnsupported = 90

var schemeRegexp = regexp.MustCompile("^(tcp|unix)://")

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return "remote"
}

func (*factory) Usage() string {
	return "remote"
}

func (*factory) Priority(ctx context.Context, endpoint string, api dockerclient.APIClient) int {
	if schemeRegexp.MatchString(endpoint) {
		return prioritySupported
	}
	return priorityUnsupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if len(cfg.Files) > 0 {
		return nil, errors.Errorf("setting config file is not supported for remote driver")
	}
	if len(cfg.BuildkitFlags) > 0 {
		return nil, errors.Errorf("setting buildkit flags is not supported for remote driver")
	}

	d := &Driver{
		factory:    f,
		InitConfig: cfg,
	}

	tls := &tlsOpts{}
	tlsEnabled := false
	for k, v := range cfg.DriverOpts {
		switch k {
		case "servername":
			tls.serverName = v
			tlsEnabled = true
		case "cacert":
			if !filepath.IsAbs(v) {
				return nil, errors.Errorf("non-absolute path '%s' provided for %s", v, k)
			}
			tls.caCert = v
			tlsEnabled = true
		case "cert":
			if !filepath.IsAbs(v) {
				return nil, errors.Errorf("non-absolute path '%s' provided for %s", v, k)
			}
			tls.cert = v
			tlsEnabled = true
		case "key":
			if !filepath.IsAbs(v) {
				return nil, errors.Errorf("non-absolute path '%s' provided for %s", v, k)
			}
			tls.key = v
			tlsEnabled = true
		default:
			return nil, errors.Errorf("invalid driver option %s for remote driver", k)
		}
	}

	if tlsEnabled {
		if tls.serverName == "" {
			// guess servername as hostname of target address
			uri, err := url.Parse(cfg.EndpointAddr)
			if err != nil {
				return nil, err
			}
			tls.serverName = uri.Hostname()
		}
		missing := []string{}
		if tls.caCert == "" {
			missing = append(missing, "cacert")
		}
		if tls.cert == "" {
			missing = append(missing, "cert")
		}
		if tls.key == "" {
			missing = append(missing, "key")
		}
		if len(missing) > 0 {
			return nil, errors.Errorf("tls enabled, but missing keys %s", strings.Join(missing, ", "))
		}
		d.tlsOpts = tls
	}

	return d, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}
