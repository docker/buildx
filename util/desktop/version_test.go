package desktop

import (
	"runtime/debug"
	"sync"
	"testing"

	"github.com/docker/buildx/version"
	"github.com/stretchr/testify/assert"
)

func TestBuildxVersionLinked(t *testing.T) {
	oldVersion := version.Version
	t.Cleanup(func() {
		version.Version = oldVersion
	})
	version.Version = "v1.2.3"
	assert.Equal(t, "v1.2.3", BuildxVersion())
}

func TestBuildxVersionBuildInfo(t *testing.T) {
	oldVersion := version.Version
	oldReadBuildInfo := readBuildInfo
	t.Cleanup(func() {
		version.Version = oldVersion
		readBuildInfo = oldReadBuildInfo
	})

	var calls int
	version.Version = "v0.0.0+unknown"
	readBuildInfo = sync.OnceValues(func() (*debug.BuildInfo, bool) {
		calls++
		return &debug.BuildInfo{
			Deps: []*debug.Module{{
				Path:    version.Package,
				Version: "v1.2.3",
			}},
		}, true
	})

	assert.Equal(t, "v1.2.3", BuildxVersion())
	assert.Equal(t, "v1.2.3", BuildxVersion())
	assert.Equal(t, 1, calls)
}
