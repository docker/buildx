package resolver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegistryConfigMirrorPath(t *testing.T) {
	plainHTTP := true
	tests := []struct {
		name   string
		cfg    map[string]RegistryConfig
		scheme string
		host   string
		path   string
	}{
		{
			name:   "host only",
			cfg:    map[string]RegistryConfig{"docker.io": {Mirrors: []string{"mirror.example.com"}}},
			scheme: "https",
			host:   "mirror.example.com",
			path:   "/v2",
		},
		{
			name:   "host with path",
			cfg:    map[string]RegistryConfig{"docker.io": {Mirrors: []string{"mirror.example.com/dockerhub"}}},
			scheme: "https",
			host:   "mirror.example.com",
			path:   "/v2/dockerhub",
		},
		{
			name:   "host with path and trailing slash",
			cfg:    map[string]RegistryConfig{"docker.io": {Mirrors: []string{"mirror.example.com:5000/proxy/hub/"}}},
			scheme: "https",
			host:   "mirror.example.com:5000",
			path:   "/v2/proxy/hub",
		},
		{
			name: "plain http settings keyed by mirror host",
			cfg: map[string]RegistryConfig{
				"docker.io":          {Mirrors: []string{"mirror.example.com/dockerhub"}},
				"mirror.example.com": {PlainHTTP: &plainHTTP},
			},
			scheme: "http",
			host:   "mirror.example.com",
			path:   "/v2/dockerhub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hosts, err := NewRegistryConfig(tt.cfg)("docker.io")
			require.NoError(t, err)
			require.Len(t, hosts, 2)
			require.Equal(t, tt.scheme, hosts[0].Scheme)
			require.Equal(t, tt.host, hosts[0].Host)
			require.Equal(t, tt.path, hosts[0].Path)
			require.Equal(t, "registry-1.docker.io", hosts[1].Host)
			require.Equal(t, "/v2", hosts[1].Path)
		})
	}
}
