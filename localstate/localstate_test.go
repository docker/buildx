package localstate

import (
	"path/filepath"
	"testing"

	"github.com/docker/buildx/util/confutil"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	_ = newls(t)
}

func TestReadRef(t *testing.T) {
	l := newls(t)
	r, err := l.ReadRef(testBuilderName, testNodeName, testStateRefID)
	require.NoError(t, err)
	require.Equal(t, testStateRef, *r)
}

func TestReadGroup(t *testing.T) {
	l := newls(t)
	g, err := l.ReadGroup(testStateGroupID)
	require.NoError(t, err)
	require.Equal(t, testStateGroup, *g)
}

func TestRemoveBuilder(t *testing.T) {
	l := newls(t)
	require.NoError(t, l.RemoveBuilder(testBuilderName))
}

func TestRemoveBuilderNode(t *testing.T) {
	l := newls(t)
	require.NoError(t, l.RemoveBuilderNode(testBuilderName, testNodeName))
}

func newls(t *testing.T) *LocalState {
	t.Helper()
	tmpdir := t.TempDir()

	l, err := New(confutil.NewConfig(nil, confutil.WithDir(tmpdir)))
	require.NoError(t, err)
	require.DirExists(t, filepath.Join(tmpdir, refsDir))
	require.Equal(t, tmpdir, l.cfg.Dir())

	require.NoError(t, l.SaveRef(testBuilderName, testNodeName, testStateRefID, testStateRef))

	require.NoError(t, l.SaveGroup(testStateGroupID, testStateGroup))
	require.NoError(t, l.SaveRef(testBuilderName, testNodeName, testStateGroupRef1ID, testStateGroupRef1))
	require.NoError(t, l.SaveRef(testBuilderName, testNodeName, testStateGroupRef2ID, testStateGroupRef2))
	require.NoError(t, l.SaveRef(testBuilderName, testNodeName, testStateGroupRef3ID, testStateGroupRef3))

	return l
}

var (
	testBuilderName = "builder"
	testNodeName    = "builder0"

	testStateRefID = "32n3ffqrxjw41ok5zxd2qhume"
	testStateRef   = State{
		Target:         "default",
		LocalPath:      "/home/foo/github.com/docker/docker-bake-action",
		DockerfilePath: "/home/foo/github.com/docker/docker-bake-action/dev.Dockerfile",
	}

	testStateGroupID = "kvqs0sgly2rmitz84r25u9qd0"
	testStateGroup   = StateGroup{
		Definition: []byte(`{"group":{"default":{"targets":["pre-checkin"]},"pre-checkin":{"targets":["vendor-update","format","build"]}},"target":{"build":{"context":".","dockerfile":"dev.Dockerfile","target":"build-update","platforms":["linux/amd64"],"output":["."]},"format":{"context":".","dockerfile":"dev.Dockerfile","target":"format-update","platforms":["linux/amd64"],"output":["."]},"vendor-update":{"context":".","dockerfile":"dev.Dockerfile","target":"vendor-update","platforms":["linux/amd64"],"output":["."]}}}`),
		Targets:    []string{"pre-checkin"},
		Inputs:     []string{"*.platform=linux/amd64"},
		Refs:       []string{"builder/builder0/hx2qf1w11qvz1x3k471c5i8xw", "builder/builder0/968zj0g03jmlx0s8qslnvh6rl", "builder/builder0/naf44f9i1710lf7y12lv5hb1z"},
	}

	testStateGroupRef1ID = "hx2qf1w11qvz1x3k471c5i8xw"
	testStateGroupRef1   = State{
		Target:         "format",
		LocalPath:      "/home/foo/github.com/docker/docker-bake-action",
		DockerfilePath: "/home/foo/github.com/docker/docker-bake-action/dev.Dockerfile",
		GroupRef:       "kvqs0sgly2rmitz84r25u9qd0",
	}

	testStateGroupRef2ID = "968zj0g03jmlx0s8qslnvh6rl"
	testStateGroupRef2   = State{
		Target:         "build",
		LocalPath:      "/home/foo/github.com/docker/docker-bake-action",
		DockerfilePath: "/home/foo/github.com/docker/docker-bake-action/dev.Dockerfile",
		GroupRef:       "kvqs0sgly2rmitz84r25u9qd0",
	}

	testStateGroupRef3ID = "naf44f9i1710lf7y12lv5hb1z"
	testStateGroupRef3   = State{
		Target:         "vendor-update",
		LocalPath:      "/home/foo/github.com/docker/docker-bake-action",
		DockerfilePath: "/home/foo/github.com/docker/docker-bake-action/dev.Dockerfile",
		GroupRef:       "kvqs0sgly2rmitz84r25u9qd0",
	}
)
