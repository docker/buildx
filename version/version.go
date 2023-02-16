package version

import (
	"runtime/debug"
	"strconv"
)

const (
	defaultVersion = "v0.0.0+unknown"
)

var (
	// Package is filled at linking time
	Package = "github.com/docker/buildx"

	// Version holds the complete version number. Filled in at linking time.
	Version = defaultVersion

	// Revision is filled with the VCS (e.g. git) revision being used to build
	// the program at linking time.
	Revision = ""
)

func init() {
	if Revision != "" {
		return
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		revision := ""
		modified := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified, _ = strconv.ParseBool(setting.Value)
			}
		}
		if revision != "" {
			Revision = revision
			if modified {
				Revision += ".m"
			}
		}
	}
}
