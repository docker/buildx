package context

import (
	"os"
	"testing"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config/configfile"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultContextInitializer(t *testing.T) {
	cli, err := command.NewDockerCli()
	require.NoError(t, err)
	os.Setenv("KUBECONFIG", "./fixtures/test-kubeconfig")
	defer os.Unsetenv("KUBECONFIG")
	ctx, err := command.ResolveDefaultContext(&cliflags.CommonOptions{}, &configfile.ConfigFile{}, command.DefaultContextStoreConfig(), cli.Err())
	require.NoError(t, err)
	assert.Equal(t, "default", ctx.Meta.Name)
	assert.Equal(t, "zoinx", ctx.Meta.Endpoints[KubernetesEndpoint].(EndpointMeta).DefaultNamespace)
}
