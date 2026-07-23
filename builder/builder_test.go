package builder

import (
	"context"
	"errors"
	"os"
	"path"
	"testing"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testFactory struct {
	name string
}

func (f testFactory) Name() string {
	return f.name
}

func (f testFactory) Usage() string {
	return f.name
}

func (testFactory) Priority(context.Context, string, dockerclient.APIClient, map[string][]string) int {
	return 10000
}

func (testFactory) New(context.Context, driver.InitConfig) (driver.Driver, error) {
	return nil, errors.New("test factory cannot create drivers")
}

func (testFactory) AllowsInstances() bool {
	return true
}

type nodeResolvingFactory struct {
	testFactory
	nodes []driver.Node
}

func (f nodeResolvingFactory) ResolveNodes(context.Context, driver.Node) ([]driver.Node, error) {
	return f.nodes, nil
}

type defaultBuilderNamerFactory struct {
	testFactory
	endpoint string
	err      error
}

func (f *defaultBuilderNamerFactory) DefaultBuilderName(_ context.Context, endpoint string) (string, error) {
	f.endpoint = endpoint
	return "", f.err
}

func TestCreateDefaultBuilderNamer(t *testing.T) {
	const endpoint = "org/builder"
	expectedErr := errors.New("default builder name")
	factory := &defaultBuilderNamerFactory{
		testFactory: testFactory{name: "test-default-builder-namer"},
		err:         expectedErr,
	}
	t.Cleanup(driver.Register(factory))

	_, err := Create(t.Context(), nil, nil, CreateOpts{
		Driver:   factory.Name(),
		Endpoint: endpoint,
	})
	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, endpoint, factory.endpoint)
}

func TestUpdateNodeGroup(t *testing.T) {
	t.Parallel()

	t.Run("Default", func(t *testing.T) {
		t.Parallel()

		factory := testFactory{name: "example"}
		ng := &store.NodeGroup{}
		node := driver.Node{
			Name:        "node",
			Endpoint:    "tcp://example:1234",
			Platforms:   []string{"linux/amd64"},
			EndpointSet: true,
			DriverOpts:  map[string]string{"key": "value"},
		}

		require.NoError(t, updateNodeGroup(t.Context(), factory, ng, node, false, nil, ""))
		require.Len(t, ng.Nodes, 1)
		assert.Equal(t, "node", ng.Nodes[0].Name)
		assert.Equal(t, "tcp://example:1234", ng.Nodes[0].Endpoint)
		assert.Equal(t, "amd64", ng.Nodes[0].Platforms[0].Architecture)
		assert.Equal(t, map[string]string{"key": "value"}, ng.Nodes[0].DriverOpts)
	})

	t.Run("Expanded", func(t *testing.T) {
		t.Parallel()

		factory := nodeResolvingFactory{
			testFactory: testFactory{
				name: "example",
			},
			nodes: []driver.Node{
				{
					Name:       "node-amd64",
					Endpoint:   "tcp://amd64:1234",
					Platforms:  []string{"linux/amd64"},
					DriverOpts: map[string]string{"arch": "amd64"},
				},
				{
					Name:       "node-arm64",
					Endpoint:   "tcp://arm64:1234",
					Platforms:  []string{"linux/arm64"},
					DriverOpts: map[string]string{"arch": "arm64"},
				},
			},
		}
		ng := &store.NodeGroup{
			Nodes: []store.Node{
				{
					Name:     "existing",
					Endpoint: "tcp://existing:1234",
				},
			},
		}

		require.NoError(t, updateNodeGroup(t.Context(), factory, ng, driver.Node{Endpoint: "org/builder"}, true, nil, ""))
		require.Len(t, ng.Nodes, 3)
		assert.Equal(t, "node-amd64", ng.Nodes[1].Name)
		assert.Equal(t, "tcp://amd64:1234", ng.Nodes[1].Endpoint)
		assert.Equal(t, "amd64", ng.Nodes[1].Platforms[0].Architecture)
		assert.Equal(t, map[string]string{"arch": "amd64"}, ng.Nodes[1].DriverOpts)
		assert.Equal(t, "node-arm64", ng.Nodes[2].Name)
		assert.Equal(t, "tcp://arm64:1234", ng.Nodes[2].Endpoint)
		assert.Equal(t, "arm64", ng.Nodes[2].Platforms[0].Architecture)
		assert.Equal(t, map[string]string{"arch": "arm64"}, ng.Nodes[2].DriverOpts)
	})

	t.Run("ExpandedUpdatePreservesEndpoint", func(t *testing.T) {
		t.Parallel()

		factory := nodeResolvingFactory{
			testFactory: testFactory{
				name: "example",
			},
			nodes: []driver.Node{
				{
					Name:        "node",
					Endpoint:    "tcp://new:1234",
					Platforms:   []string{"linux/arm64"},
					EndpointSet: false,
				},
			},
		}
		ng := &store.NodeGroup{
			Nodes: []store.Node{
				{
					Name:     "node",
					Endpoint: "tcp://existing:1234",
				},
			},
		}

		require.NoError(t, updateNodeGroup(t.Context(), factory, ng, driver.Node{Endpoint: "org/builder"}, false, nil, ""))
		require.Len(t, ng.Nodes, 1)
		assert.Equal(t, "tcp://existing:1234", ng.Nodes[0].Endpoint)
		assert.Equal(t, "arm64", ng.Nodes[0].Platforms[0].Architecture)
	})

	t.Run("ExpandedAppendRejectsNameCollision", func(t *testing.T) {
		t.Parallel()

		factory := nodeResolvingFactory{
			testFactory: testFactory{
				name: "example",
			},
			nodes: []driver.Node{
				{
					Name:       "node",
					Endpoint:   "tcp://new:1234",
					Platforms:  []string{"linux/arm64"},
					DriverOpts: map[string]string{"arch": "arm64"},
				},
			},
		}
		ng := &store.NodeGroup{
			Nodes: []store.Node{
				{
					Name:       "node",
					Endpoint:   "tcp://existing:1234",
					DriverOpts: map[string]string{"arch": "amd64"},
				},
			},
		}

		err := updateNodeGroup(t.Context(), factory, ng, driver.Node{Endpoint: "org/builder"}, true, nil, "")
		require.EqualError(t, err, `node "node" already exists`)
		require.Len(t, ng.Nodes, 1)
		assert.Equal(t, "tcp://existing:1234", ng.Nodes[0].Endpoint)
		assert.Equal(t, map[string]string{"arch": "amd64"}, ng.Nodes[0].DriverOpts)
	})

	t.Run("EmptyExpansion", func(t *testing.T) {
		t.Parallel()

		factory := nodeResolvingFactory{
			testFactory: testFactory{
				name: "example",
			},
		}

		err := updateNodeGroup(t.Context(), factory, &store.NodeGroup{}, driver.Node{}, false, nil, "")
		require.EqualError(t, err, `driver "example" returned no nodes`)
	})
}

func TestCsvToMap(t *testing.T) {
	d := []string{
		"\"tolerations=key=foo,value=bar;key=foo2,value=bar2\",replicas=1",
		"namespace=default",
	}
	r, err := csvToMap(d)

	require.NoError(t, err)

	require.Contains(t, r, "tolerations")
	require.Equal(t, "key=foo,value=bar;key=foo2,value=bar2", r["tolerations"])

	require.Contains(t, r, "replicas")
	require.Equal(t, "1", r["replicas"])

	require.Contains(t, r, "namespace")
	require.Equal(t, "default", r["namespace"])
}

func TestParseBuildkitdFlags(t *testing.T) {
	dirConf := t.TempDir()

	buildkitdConfPath := path.Join(dirConf, "buildkitd-conf.toml")
	require.NoError(t, os.WriteFile(buildkitdConfPath, []byte(`
# debug enables additional debug logging
debug = true
# insecure-entitlements allows insecure entitlements, disabled by default.
insecure-entitlements = [ "network.host", "security.insecure" ]
[log]
  # log formatter: json or text
  format = "text"
`), 0644))

	buildkitdConfBrokenPath := path.Join(dirConf, "buildkitd-conf-broken.toml")
	require.NoError(t, os.WriteFile(buildkitdConfBrokenPath, []byte(`
[worker.oci]
  gc = "maybe"
`), 0644))

	buildkitdConfUnknownFieldPath := path.Join(dirConf, "buildkitd-unknown-field.toml")
	require.NoError(t, os.WriteFile(buildkitdConfUnknownFieldPath, []byte(`
foo = "bar"
`), 0644))

	testCases := []struct {
		name                string
		flags               string
		driver              string
		driverOpts          map[string]string
		buildkitdConfigFile string
		expected            []string
		wantErr             bool
	}{
		{
			"docker-container no flags",
			"",
			"docker-container",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"kubernetes no flags",
			"",
			"kubernetes",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"remote no flags",
			"",
			"remote",
			nil,
			"",
			nil,
			false,
		},
		{
			"docker-container with insecure flag",
			"--allow-insecure-entitlement=security.insecure",
			"docker-container",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=security.insecure",
			},
			false,
		},
		{
			"docker-container with insecure and host flag",
			"--allow-insecure-entitlement=network.host --allow-insecure-entitlement=security.insecure",
			"docker-container",
			nil,
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
				"--allow-insecure-entitlement=security.insecure",
			},
			false,
		},
		{
			"docker-container with network host opt",
			"",
			"docker-container",
			map[string]string{"network": "host"},
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"docker-container with host flag and network host opt",
			"--allow-insecure-entitlement=network.host",
			"docker-container",
			map[string]string{"network": "host"},
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
		{
			"docker-container with insecure, host flag and network host opt",
			"--allow-insecure-entitlement=network.host --allow-insecure-entitlement=security.insecure",
			"docker-container",
			map[string]string{"network": "host"},
			"",
			[]string{
				"--allow-insecure-entitlement=network.host",
				"--allow-insecure-entitlement=security.insecure",
			},
			false,
		},
		{
			"docker-container with buildkitd conf setting network.host entitlement",
			"",
			"docker-container",
			nil,
			buildkitdConfPath,
			nil,
			false,
		},
		{
			"error parsing flags",
			"foo'",
			"docker-container",
			nil,
			"",
			nil,
			true,
		},
		{
			"error parsing buildkit config",
			"",
			"docker-container",
			nil,
			buildkitdConfBrokenPath,
			nil,
			true,
		},
		{
			"unknown field in buildkit config",
			"",
			"docker-container",
			nil,
			buildkitdConfUnknownFieldPath,
			[]string{
				"--allow-insecure-entitlement=network.host",
			},
			false,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			flags, err := parseBuildkitdFlags(tt.flags, tt.driver, tt.driverOpts, tt.buildkitdConfigFile)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, flags)
		})
	}
}
