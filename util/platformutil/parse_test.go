package platformutil

import (
	"testing"

	"github.com/containerd/platforms"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestDedupePreservesOSVersionAndFeatures(t *testing.T) {
	t.Parallel()

	got := Dedupe([]ocispecs.Platform{
		platforms.MustParse("windows(10.0.17763)/amd64"),
		platforms.MustParse("windows(10.0.20348)/amd64"),
		platforms.MustParse("windows(10.0.20348+win32k)/amd64"),
		platforms.MustParse("windows(10.0.17763)/amd64"),
		platforms.MustParse("linux/x86_64"),
		platforms.MustParse("linux/amd64"),
	})

	require.Equal(t, []string{
		"windows(10.0.17763)/amd64",
		"windows(10.0.20348)/amd64",
		"windows(10.0.20348+win32k)/amd64",
		"linux/amd64",
	}, Format(got))
}

func TestFormatInGroupsPreservesOSVersionAndFeatures(t *testing.T) {
	t.Parallel()

	got := FormatInGroups(
		[]ocispecs.Platform{
			platforms.MustParse("windows(10.0.17763)/amd64"),
			platforms.MustParse("windows(10.0.20348)/amd64"),
		},
		[]ocispecs.Platform{
			platforms.MustParse("windows(10.0.20348)/amd64"),
			platforms.MustParse("windows(10.0.20348+win32k)/amd64"),
			platforms.MustParse("linux/x86_64"),
			platforms.MustParse("linux/amd64"),
		},
	)

	require.Equal(t, []string{
		"windows(10.0.17763)/amd64*",
		"windows(10.0.20348)/amd64*",
		"windows(10.0.20348+win32k)/amd64",
		"linux/amd64",
	}, got)
}

func TestFormatPreservesOSVersionAndFeatures(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{
		"windows(10.0.17763)/amd64",
		"windows(10.0.20348+win32k)/amd64",
		"linux/amd64",
		"linux/arm/v7",
	}, Format([]ocispecs.Platform{
		platforms.MustParse("windows(10.0.17763)/amd64"),
		platforms.MustParse("windows(10.0.20348+win32k)/amd64"),
		platforms.MustParse("linux/x86_64"),
		platforms.MustParse("linux/arm/v7"),
	}))

	require.Nil(t, Format(nil))
}
