package replay

import (
	"testing"

	replaypkg "github.com/docker/buildx/replay"
	"github.com/stretchr/testify/require"
)

func TestResolveSnapshotOutputRejectsRegistry(t *testing.T) {
	_, err := resolveSnapshotOutput([]string{"type=registry,name=example.com/replay:test"})
	require.Error(t, err)
	var notImplemented *replaypkg.NotImplementedError
	require.ErrorAs(t, err, &notImplemented)
}
