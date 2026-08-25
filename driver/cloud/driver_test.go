package cloud

import (
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/buildx/driver"
	"github.com/moby/moby/api/types/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfo(t *testing.T) {
	t.Parallel()

	t.Run("Running", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)

		info, err := (&Driver{healthAddress: server.URL}).Info(t.Context())

		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, driver.Running, info.Status)
	})

	t.Run("StatusError", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("x-trace-id", "trace-123")
			http.Error(w, "denied", http.StatusForbidden)
		}))
		t.Cleanup(server.Close)

		info, err := (&Driver{healthAddress: server.URL}).Info(t.Context())

		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "403 Forbidden")
		assert.Contains(t, err.Error(), "trace-123")
		assert.Contains(t, err.Error(), "denied")
	})

	t.Run("TLSServerName", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(server.Close)

		caPath := filepath.Join(t.TempDir(), "ca.pem")
		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: server.Certificate().Raw,
		})
		require.NoError(t, os.WriteFile(caPath, certPEM, 0600))

		serverURL, err := url.Parse(server.URL)
		require.NoError(t, err)
		_, port, err := net.SplitHostPort(serverURL.Host)
		require.NoError(t, err)

		info, err := (&Driver{
			healthAddress: "https://localhost:" + port,
			tls: tlsOpts{
				caCert:     caPath,
				serverName: "example.com",
			},
		}).Info(t.Context())

		require.NoError(t, err)
		require.NotNil(t, info)
		assert.Equal(t, driver.Running, info.Status)
	})
}

type registryTokenSupport struct {
	os       string
	version  string
	driver   string
	expected bool
}

func TestRegistryTokenSupport(t *testing.T) {
	t.Parallel()

	table := []registryTokenSupport{
		{
			os:       "Docker Desktop",
			version:  "master",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "25.0.0",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "28.0.0-rc.1",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "25.0.0-rc.1",
			driver:   "io.containerd.snapshotter.v1",
			expected: false,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "24.0.6",
			driver:   "io.containerd.snapshotter.v1",
			expected: false,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "master",
			driver:   "io.containerd.snapshotter.v1",
			expected: true,
		},
		{
			os:       "Linux Mint 21.1",
			version:  "24.0.6",
			driver:   "docker",
			expected: true,
		},
	}

	for i, c := range table {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			t.Parallel()

			actual, err := daemonInfoSupportsRegistryTokenPull(system.Info{
				ServerVersion:   c.version,
				OperatingSystem: c.os,
				DriverStatus: [][2]string{
					{"driver-type", c.driver},
				},
			})
			require.NoError(t, err)
			assert.Equal(t, c.expected, actual)
		})
	}
}
