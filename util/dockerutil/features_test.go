package dockerutil

import (
	"testing"

	"github.com/moby/moby/api/types/system"
	"github.com/stretchr/testify/assert"
)

func TestHasOCIImporter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info system.Info
		want bool
	}{
		{
			name: "ContainerdSnapshotter",
			info: system.Info{
				DriverStatus: [][2]string{
					{"driver-type", "io.containerd.snapshotter.v1"},
				},
			},
			want: true,
		},
		{
			name: "DockerDriver",
			info: system.Info{
				DriverStatus: [][2]string{
					{"driver-type", "docker"},
				},
			},
		},
		{
			name: "UnrelatedStatus",
			info: system.Info{
				DriverStatus: [][2]string{
					{"Backing Filesystem", "extfs"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, HasOCIImporter(tt.info))
		})
	}
}
