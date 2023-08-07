package workers

import (
	"testing"

	"github.com/moby/buildkit/util/testutil/integration"
)

var features = map[string]struct{}{}

func CheckFeatureCompat(t *testing.T, sb integration.Sandbox, reason ...string) {
	integration.CheckFeatureCompat(t, sb, features, reason...)
}
