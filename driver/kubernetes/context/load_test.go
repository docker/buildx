package context

import (
	"os"
	"testing"

	"github.com/docker/cli/cli/command"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultContextInitializer(t *testing.T) {
	os.Setenv("KUBECONFIG", "./fixtures/test-kubeconfig")
	defer os.Unsetenv("KUBECONFIG")
	ctx, err := command.ResolveDefaultContext(&cliflags.CommonOptions{}, command.DefaultContextStoreConfig())
	require.NoError(t, err)
	assert.Equal(t, "default", ctx.Meta.Name)
	assert.Equal(t, "zoinx", ctx.Meta.Endpoints[KubernetesEndpoint].(EndpointMeta).DefaultNamespace)
}
