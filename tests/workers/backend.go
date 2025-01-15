package workers

import (
	"os"
	"strings"

	"github.com/moby/buildkit/util/testutil/integration"
)

type backend struct {
	builder             string
	context             string
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

func (s backend) Supports(feature string) bool {
	if enabledFeatures := os.Getenv("BUILDKIT_TEST_ENABLE_FEATURES"); enabledFeatures != "" {
		for _, enabledFeature := range strings.Split(enabledFeatures, ",") {
			if feature == enabledFeature {
				return true
			}
		}
	}
	if disabledFeatures := os.Getenv("BUILDKIT_TEST_DISABLE_FEATURES"); disabledFeatures != "" {
		for _, disabledFeature := range strings.Split(disabledFeatures, ",") {
			if feature == disabledFeature {
				return false
			}
		}
	}
	for _, unsupportedFeature := range s.unsupportedFeatures {
		if feature == unsupportedFeature {
			return false
		}
	}
	return true
}
