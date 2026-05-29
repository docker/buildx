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
		var ips []string
		if ip == mobyHostGatewayName {
			hgip, err := nodeDriver.HostGatewayIP(ctx)
			if err != nil {
				return "", errors.Wrap(err, "unable to derive the IP value for host-gateway")
			}
			ips = append(ips, hgip.String())
		} else {
			for v := range strings.SplitSeq(ip, ",") {
				// If the address is enclosed in square brackets, extract it
				// (for IPv6, but permit it for IPv4 as well; we don't know the
				// address family here, but it's unambiguous).
				if len(v) > 2 && v[0] == '[' && v[len(v)-1] == ']' {
					v = v[1 : len(v)-1]
				}
				if net.ParseIP(v) == nil {
					return "", errors.Errorf("invalid host %s", h)
				}
				ips = append(ips, v)
			}
		}
		for _, v := range ips {
			hosts = append(hosts, host+"="+v)
		}
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

// User-facing resource keys accepted in `--resource key=value` entries, mirroring docker run flag names.
const (
	resourceKeyMemory     = "memory"
	resourceKeyMemorySwap = "memory-swap"
	resourceKeyCPUShares  = "cpu-shares"
	resourceKeyCPUPeriod  = "cpu-period"
	resourceKeyCPUQuota   = "cpu-quota"
	resourceKeyCPUSetCPUs = "cpuset-cpus"
	resourceKeyCPUSetMems = "cpuset-mems"
)

// Frontend attribute keys, must match those parsed by BuildKit's dockerui frontend.
const (
	attrMemory     = "memory"
	attrMemorySwap = "memswap"
	attrCPUShares  = "cpushares"
	attrCPUPeriod  = "cpuperiod"
	attrCPUQuota   = "cpuquota"
	attrCPUSetCPUs = "cpusetcpus"
	attrCPUSetMems = "cpusetmems"
)

// ParseResourceLimits parses `key=value` entries from the `--resource` flag into ResourceLimits.
func ParseResourceLimits(entries []string) (ResourceLimits, error) {
	var rl ResourceLimits
	for _, entry := range entries {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			return rl, errors.Errorf("invalid resource %q, expected key=value", entry)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case resourceKeyMemory:
			if err := rl.Memory.Set(v); err != nil {
				return rl, errors.Wrapf(err, "invalid value %q for resource %s", v, k)
			}
		case resourceKeyMemorySwap:
			if err := rl.MemorySwap.Set(v); err != nil {
				return rl, errors.Wrapf(err, "invalid value %q for resource %s", v, k)
			}
		case resourceKeyCPUShares, resourceKeyCPUPeriod, resourceKeyCPUQuota:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return rl, errors.Wrapf(err, "invalid value %q for resource %s", v, k)
			}
			switch k {
			case resourceKeyCPUShares:
				rl.CPUShares = n
			case resourceKeyCPUPeriod:
				rl.CPUPeriod = n
			case resourceKeyCPUQuota:
				rl.CPUQuota = n
			}
		case resourceKeyCPUSetCPUs:
			rl.CPUSetCPUs = v
		case resourceKeyCPUSetMems:
			rl.CPUSetMems = v
		default:
			return rl, errors.Errorf("unknown resource %q", k)
		}
	}
	return rl, nil
}

// addResourceLimits sets the frontend attributes for the resource limits.
// Only non-zero values are sent, so builds against a daemon without the feature keep working.
func addResourceLimits(rl ResourceLimits, attrs map[string]string) {
	if v := rl.Memory.Value(); v > 0 {
		attrs[attrMemory] = strconv.FormatInt(v, 10)
	}
	if v := rl.MemorySwap.Value(); v != 0 {
		attrs[attrMemorySwap] = strconv.FormatInt(v, 10)
	}
	if rl.CPUShares > 0 {
		attrs[attrCPUShares] = strconv.FormatInt(rl.CPUShares, 10)
	}
	if rl.CPUPeriod > 0 {
		attrs[attrCPUPeriod] = strconv.FormatInt(rl.CPUPeriod, 10)
	}
	if rl.CPUQuota > 0 {
		attrs[attrCPUQuota] = strconv.FormatInt(rl.CPUQuota, 10)
	}
	if rl.CPUSetCPUs != "" {
		attrs[attrCPUSetCPUs] = rl.CPUSetCPUs
	}
	if rl.CPUSetMems != "" {
		attrs[attrCPUSetMems] = rl.CPUSetMems
	}
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
