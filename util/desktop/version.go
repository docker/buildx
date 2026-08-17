package desktop

import (
	"runtime/debug"
	"sync"

	"github.com/docker/buildx/version"
)

var readBuildInfo = sync.OnceValues(debug.ReadBuildInfo)

func BuildxVersion() string {
	if version.Version != "v0.0.0+unknown" {
		return version.Version
	}
	info, ok := readBuildInfo()
	if !ok || info == nil {
		return version.Version
	}
	for _, dep := range info.Deps {
		if dep.Path != version.Package {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		if dep.Version != "" {
			return dep.Version
		}
	}
	return version.Version
}
