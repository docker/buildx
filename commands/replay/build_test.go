package replay

import (
	"testing"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/replay"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestFilterSubjectsByPlatform(t *testing.T) {
	amd := &replay.Subject{Descriptor: ocispecs.Descriptor{Platform: &ocispecs.Platform{OS: "linux", Architecture: "amd64"}}}
	arm := &replay.Subject{Descriptor: ocispecs.Descriptor{Platform: &ocispecs.Platform{OS: "linux", Architecture: "arm64"}}}
	subjects := []*replay.Subject{amd, arm}

	// "all" keeps every subject.
	out, err := filterSubjectsByPlatform(subjects, []string{"all"})
	require.NoError(t, err)
	require.Len(t, out, 2)

	// An empty filter collapses to the host's default platform, which
	// must match exactly one of our fake subjects.
	out, err = filterSubjectsByPlatform(subjects, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	hostArch := platforms.DefaultSpec().Architecture
	require.Equal(t, hostArch, out[0].Descriptor.Platform.Architecture)

	// Explicit match on an alternate platform.
	other := "arm64"
	if hostArch == "arm64" {
		other = "amd64"
	}
	out, err = filterSubjectsByPlatform(subjects, []string{"linux/" + other})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, other, out[0].Descriptor.Platform.Architecture)

	// Explicit platform with no matching subject is an error.
	_, err = filterSubjectsByPlatform([]*replay.Subject{amd}, []string{"linux/arm64"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not present")
}
