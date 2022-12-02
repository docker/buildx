package build

import (
	"archive/tar"
	"bytes"
	"net"
	"os"
	"strings"

	"github.com/docker/cli/opts"
	"github.com/pkg/errors"
)

const (
	// archiveHeaderSize is the number of bytes in an archive header
	archiveHeaderSize = 512
	// mobyHostGatewayName defines a special string which users can append to
	// --add-host to add an extra entry in /etc/hosts that maps
	// host.docker.internal to the host IP
	mobyHostGatewayName = "host-gateway"
)

func isLocalDir(c string) bool {
	st, err := os.Stat(c)
	return err == nil && st.IsDir()
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
func toBuildkitExtraHosts(inp []string, mobyDriver bool) (string, error) {
	if len(inp) == 0 {
		return "", nil
	}
	hosts := make([]string, 0, len(inp))
	for _, h := range inp {
		host, ip, ok := strings.Cut(h, ":")
		if !ok || host == "" || ip == "" {
			return "", errors.Errorf("invalid host %s", h)
		}
		// Skip IP address validation for "host-gateway" string with moby driver
		if !mobyDriver || ip != mobyHostGatewayName {
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
