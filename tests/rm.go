package tests

import (
	"os"
	"strings"
	"testing"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/confutil"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func rmCmd(sb integration.Sandbox, opts ...cmdOpt) (string, error) {
	opts = append([]cmdOpt{withArgs("rm")}, opts...)
	cmd := buildxCmd(sb, opts...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var rmTests = []func(t *testing.T, sb integration.Sandbox){
	testRm,
	testRmMulti,
	testRmInvalidBuildkitdConfig,
	testRmAllInactiveInvalidBuildkitdConfig,
}

func testRm(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	out, err := rmCmd(sb, withArgs("default"))
	require.Error(t, err, out) // can't remove a docker builder

	out, err = createCmd(sb, withArgs("--driver", "docker-container"))
	require.NoError(t, err, out)
	builderName := strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
	require.NoError(t, err, out)

	out, err = rmCmd(sb, withArgs(builderName))
	require.NoError(t, err, out)
}

func testRmMulti(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	var builderNames []string
	for range 3 {
		out, err := createCmd(sb, withArgs("--driver", "docker-container"))
		require.NoError(t, err, out)
		builderName := strings.TrimSpace(out)

		out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
		require.NoError(t, err, out)
		builderNames = append(builderNames, builderName)
	}

	out, err := rmCmd(sb, withArgs(builderNames...))
	require.NoError(t, err, out)
}

func testRmInvalidBuildkitdConfig(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	out, err := createCmd(sb, withArgs("--driver", "docker-container"))
	require.NoError(t, err, out)
	builderName := strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
	require.NoError(t, err, out)

	var container string
	t.Cleanup(func() {
		if builderName != "" {
			_, _ = rmCmd(sb, withArgs("--keep-daemon", builderName))
		}
		if container != "" {
			_ = dockerCmd(sb, withArgs("container", "rm", "-f", container)).Run()
		}
	})

	updateStoredBuilder(t, sb, builderName, func(ng *store.NodeGroup) {
		require.NotEmpty(t, ng.Nodes)
		container = driver.BuilderName(ng.Nodes[0].Name)

		if ng.Nodes[0].Files == nil {
			ng.Nodes[0].Files = map[string][]byte{}
		}
		ng.Nodes[0].Files["buildkitd.toml"] = []byte(`
[worker.oci]
  gc = "maybe"
`)
	})

	out, err = rmCmd(sb, withArgs(builderName))
	require.NoError(t, err, out)
	require.Contains(t, out, builderName+" removed")
	requireNoStoredBuilder(t, sb, builderName)
	requireNoContainer(t, sb, container)
	builderName = ""
}

func testRmAllInactiveInvalidBuildkitdConfig(t *testing.T, sb integration.Sandbox) {
	if !isDockerContainerWorker(sb) {
		t.Skip("only testing with docker-container worker")
	}

	out, err := createCmd(sb, withArgs("--driver", "docker-container"))
	require.NoError(t, err, out)
	builderName := strings.TrimSpace(out)

	out, err = inspectCmd(sb, withArgs(builderName, "--bootstrap"))
	require.NoError(t, err, out)

	var container string
	t.Cleanup(func() {
		if builderName != "" {
			_, _ = rmCmd(sb, withArgs("--keep-daemon", builderName))
		}
		if container != "" {
			_ = dockerCmd(sb, withArgs("container", "rm", "-f", container)).Run()
		}
	})

	updateStoredBuilder(t, sb, builderName, func(ng *store.NodeGroup) {
		require.NotEmpty(t, ng.Nodes)
		container = driver.BuilderName(ng.Nodes[0].Name)

		if ng.Nodes[0].Files == nil {
			ng.Nodes[0].Files = map[string][]byte{}
		}
		ng.Nodes[0].Files["buildkitd.toml"] = []byte(`
[worker.oci]
  gc = "maybe"
`)
	})

	cmd := dockerCmd(sb, withArgs("container", "stop", container))
	require.NoError(t, cmd.Run())

	out, err = rmCmd(sb, withArgs("--all-inactive", "--force"))
	require.NoError(t, err, out)
	require.Contains(t, out, builderName+" removed")
	requireNoStoredBuilder(t, sb, builderName)
	requireNoContainer(t, sb, container)
	builderName = ""
}

func updateStoredBuilder(t *testing.T, sb integration.Sandbox, name string, fn func(*store.NodeGroup)) {
	t.Helper()

	st, err := store.New(confutil.NewConfig(nil, confutil.WithDir(buildxConfig(sb))))
	require.NoError(t, err)

	txn, release, err := st.Txn()
	require.NoError(t, err)
	defer release()

	ng, err := txn.NodeGroupByName(name)
	require.NoError(t, err)

	fn(ng)
	require.NoError(t, txn.Save(ng))
}

func requireNoStoredBuilder(t *testing.T, sb integration.Sandbox, name string) {
	t.Helper()

	st, err := store.New(confutil.NewConfig(nil, confutil.WithDir(buildxConfig(sb))))
	require.NoError(t, err)

	txn, release, err := st.Txn()
	require.NoError(t, err)
	defer release()

	_, err = txn.NodeGroupByName(name)
	require.Error(t, err)
	require.True(t, os.IsNotExist(errors.Cause(err)), "expected builder %q to be removed: %v", name, err)
}

func requireNoContainer(t *testing.T, sb integration.Sandbox, name string) {
	t.Helper()

	cmd := dockerCmd(sb, withArgs("container", "inspect", name))
	require.Error(t, cmd.Run())
}
