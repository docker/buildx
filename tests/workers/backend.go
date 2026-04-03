package workers

import (
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/moby/buildkit/util/testutil/integration"
)

type backend struct {
	builder             string
	context             string
	registryHost        string
	registryPorts       []int
	registryPortIndex   int
	registryMu          sync.Mutex
	unsupportedFeatures []string
}

var _ integration.Backend = &backend{}

func (s *backend) Address() string {
	return s.builder
}

func (s *backend) DebugAddress() string {
	return ""
}

func (s *backend) DockerAddress() string {
	return s.context
}

func (s *backend) ContainerdAddress() string {
	return ""
}

func (s *backend) Snapshotter() string {
	return ""
}

func (s *backend) Rootless() bool {
	return false
}

func (s *backend) NetNSDetached() bool {
	return false
}

func (s *backend) ExtraEnv() []string {
	return nil
}

func (s *backend) NewRegistry() (string, func() error, error) {
	if s.registryHost == "" || len(s.registryPorts) == 0 {
		url, cl, err := integration.NewRegistry("")
		if err != nil {
			return "", nil, err
		}
		return s.RewriteRegistryAddress(url), cl, nil
	}

	s.registryMu.Lock()
	defer s.registryMu.Unlock()

	if s.registryPortIndex >= len(s.registryPorts) {
		return "", nil, fmt.Errorf("exhausted kubernetes registry port pool")
	}
	port := s.registryPorts[s.registryPortIndex]
	s.registryPortIndex++

	_, cl, err := integration.NewRegistryAt("", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
	if err != nil {
		return "", nil, err
	}
	return net.JoinHostPort(s.registryHost, strconv.Itoa(port)), cl, nil
}

func (s *backend) RewriteRegistryAddress(in string) string {
	if s.registryHost == "" {
		return in
	}
	host, port, err := net.SplitHostPort(in)
	if err != nil {
		return in
	}
	if host != "localhost" && host != "127.0.0.1" {
		return in
	}
	return net.JoinHostPort(s.registryHost, port)
}

func (s backend) Supports(feature string) bool {
	if enabledFeatures := os.Getenv("BUILDKIT_TEST_ENABLE_FEATURES"); enabledFeatures != "" {
		if slices.Contains(strings.Split(enabledFeatures, ","), feature) {
			return true
		}
	}
	if disabledFeatures := os.Getenv("BUILDKIT_TEST_DISABLE_FEATURES"); disabledFeatures != "" {
		if slices.Contains(strings.Split(disabledFeatures, ","), feature) {
			return false
		}
	}
	return !slices.Contains(s.unsupportedFeatures, feature)
}
