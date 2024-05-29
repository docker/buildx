package build

import (
	"archive/tar"
	"bytes"
	"context"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/docker/buildx/driver"
	"github.com/docker/cli/opts"
	"github.com/moby/buildkit/util/gitutil"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// archiveHeaderSize is the number of bytes in an archive header
	archiveHeaderSize = 512
	// mobyHostGatewayName defines a special string which users can append to
	// --add-host to add an extra entry in /etc/hosts that maps
	// host.docker.internal to the host IP
	mobyHostGatewayName = "host-gateway"
)

// isHTTPURL returns true if the provided str is an HTTP(S) URL by checking if it
// has a http:// or https:// scheme. No validation is performed to verify if the
// URL is well-formed.
func isHTTPURL(str string) bool {
	return strings.HasPrefix(str, "https://") || strings.HasPrefix(str, "http://")
}

func IsRemoteURL(c string) bool {
	if isHTTPURL(c) {
		return true
	}
	if _, err := gitutil.ParseGitRef(c); err == nil {
		return true
	}
	return false
}

func isArchive(header []byte) bool {
	for _, m := range [][]byte{
		{0x42, 0x5A, 0x68},                   // bzip2
		{0x1F, 0x8B, 0x08},                   // gzip
		{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}, // xz
	} {
		if len(header) < len(m) {
			continue
		}
		if bytes.Equal(m, header[:len(m)]) {
			return true
		}
	}

	r := tar.NewReader(bytes.NewBuffer(header))
	_, err := r.Next()
	return err == nil
}

// toBuildkitExtraHosts converts hosts from docker key:value format to buildkit's csv format
func toBuildkitExtraHosts(ctx context.Context, inp []string, nodeDriver *driver.DriverHandle) (string, error) {
	if len(inp) == 0 {
		return "", nil
	}
	hosts := make([]string, 0, len(inp))
	for _, h := range inp {
		host, ip, ok := strings.Cut(h, "=")
		if !ok {
			host, ip, ok = strings.Cut(h, ":")
		}
		if !ok || host == "" || ip == "" {
			return "", errors.Errorf("invalid host %s", h)
		}
		// If the IP Address is a "host-gateway", replace this value with the
		// IP address provided by the worker's label.
		if ip == mobyHostGatewayName {
			hgip, err := nodeDriver.HostGatewayIP(ctx)
			if err != nil {
				return "", errors.Wrap(err, "unable to derive the IP value for host-gateway")
			}
			ip = hgip.String()
		} else {
			// If the address is enclosed in square brackets, extract it (for IPv6, but
			// permit it for IPv4 as well; we don't know the address family here, but it's
			// unambiguous).
			if len(ip) > 2 && ip[0] == '[' && ip[len(ip)-1] == ']' {
				ip = ip[1 : len(ip)-1]
			}
			if net.ParseIP(ip) == nil {
				return "", errors.Errorf("invalid host %s", h)
			}
		}
		hosts = append(hosts, host+"="+ip)
	}
	return strings.Join(hosts, ","), nil
}

// toBuildkitUlimits converts ulimits from docker type=soft:hard format to buildkit's csv format
func toBuildkitUlimits(inp *opts.UlimitOpt) (string, error) {
	if inp == nil || len(inp.GetList()) == 0 {
		return "", nil
	}
	ulimits := make([]string, 0, len(inp.GetList()))
	for _, ulimit := range inp.GetList() {
		ulimits = append(ulimits, ulimit.String())
	}
	return strings.Join(ulimits, ","), nil
}

func notSupported(f driver.Feature, d *driver.DriverHandle, docs string) error {
	return errors.Errorf(`%s is not supported for the %s driver.
Switch to a different driver, or turn on the containerd image store, and try again.
Learn more at %s`, f, d.Factory().Name(), docs)
}

func noDefaultLoad() bool {
	v, ok := os.LookupEnv("BUILDX_NO_DEFAULT_LOAD")
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		logrus.Warnf("invalid non-bool value for BUILDX_NO_DEFAULT_LOAD: %s", v)
	}
	return b
}
