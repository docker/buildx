package dockerutil

import (
	"slices"

	"github.com/moby/moby/api/types/system"
)

type Feature string

const OCIImporter Feature = "OCI importer"

func HasOCIImporter(info system.Info) bool {
	return slices.ContainsFunc(info.DriverStatus, func(status [2]string) bool {
		return status[0] == "driver-type" && status[1] == "io.containerd.snapshotter.v1"
	})
}
