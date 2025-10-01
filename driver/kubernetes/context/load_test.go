package context

import (
	"testing"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/context/store"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultContextInitializer(t *testing.T) {
	t.Setenv("KUBECONFIG", "./fixtures/test-kubeconfig")
	dockerCLI, err := command.NewDockerCli()
	require.NoError(t, err)

	// FIXME(thaJeztah): the context-store is not initialized until "Initialize" is called.
	err = dockerCLI.Initialize(cliflags.NewClientOptions())
	require.NoError(t, err)

	cs := dockerCLI.ContextStore()
	assert.NotNil(t, cs)
	meta, err := cs.GetMetadata(command.DefaultContextName)
	require.NoError(t, err)

	assert.Equal(t, "default", meta.Name)
	assert.Equal(t, "zoinx", meta.Endpoints[KubernetesEndpoint].(EndpointMeta).DefaultNamespace)
}

func TestConfigFromEndpoint(t *testing.T) {
	t.Setenv("KUBECONFIG", "./fixtures/test-kubeconfig")
	cfg, err := ConfigFromEndpoint(
		"kubernetes:///buildx-test-4c972a3f9d369614b40f28a281790c7e?deployment=buildkit-4c2ed3ed-970f-4f3d-a6df-a4fcbab4d5cf-d9d73&kubeconfig=.%2Ffixtures%2Fk3s-kubeconfig",
		store.New(config.ContextStoreDir(), command.DefaultContextStoreConfig()),
	)
	require.NoError(t, err)
	rawcfg, err := cfg.RawConfig()
	require.NoError(t, err)
	ctxcfg := "k3s"
	if _, ok := rawcfg.Contexts[ctxcfg]; !ok {
		t.Errorf("Context config %q not found", ctxcfg)
	}
}
