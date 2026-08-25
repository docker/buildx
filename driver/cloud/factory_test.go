package cloud

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultBuilderName(t *testing.T) {
	t.Parallel()

	factory := &factory{}
	namer, ok := any(factory).(driver.DefaultBuilderNamer)
	require.True(t, ok)
	name, err := namer.DefaultBuilderName(t.Context(), "Org/Builder")
	require.NoError(t, err)
	assert.Equal(t, "cloud-org-builder", name)

	name, err = namer.DefaultBuilderName(t.Context(), "cloud://Org/Builder")
	require.NoError(t, err)
	assert.Equal(t, "cloud-org-builder", name)

	_, err = namer.DefaultBuilderName(t.Context(), "Org")
	require.EqualError(t, err, "builder should be in the format: <account>/<builder>")
}

func TestVerifyDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{
			name:   "DockerURL",
			rawURL: "tcp://build-cloud.docker.com:443",
		},
		{
			name:   "MixedCaseDockerURL",
			rawURL: "tcp://BUILD-CLOUD.DOCKER.COM:443",
		},
		{
			name:   "DockerHostPort",
			rawURL: "build-cloud.docker.com:443",
		},
		{
			name:   "DockerHubURL",
			rawURL: "https://index.docker.io/v1/",
		},
		{
			name:   "LocalhostPort",
			rawURL: "localhost:5000",
		},
		{
			name:   "SingleLabelHost",
			rawURL: "internal-registry",
		},
		{
			name:    "Empty",
			rawURL:  "",
			wantErr: true,
		},
		{
			name:    "ExternalURL",
			rawURL:  "tcp://example.net:443",
			wantErr: true,
		},
		{
			name:    "ExternalHostPort",
			rawURL:  "example.net:443",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := verifyDomain(tt.rawURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestEndpointHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{
			name:   "TCP",
			rawURL: "tcp://build-cloud.docker.com:443",
			want:   "build-cloud.docker.com:443",
		},
		{
			name:   "HTTPS",
			rawURL: "https://build-cloud.docker.com:443",
			want:   "build-cloud.docker.com:443",
		},
		{
			name:   "BareHostPort",
			rawURL: "build-cloud.docker.com:443",
			want:   "build-cloud.docker.com:443",
		},
		{
			name:   "IPv4",
			rawURL: "127.0.0.1:5000",
			want:   "127.0.0.1:5000",
		},
		{
			name:   "IPv6",
			rawURL: "[::1]:5000",
			want:   "[::1]:5000",
		},
		{
			name:   "MixedCaseDNS",
			rawURL: "tcp://BUILD-CLOUD.DOCKER.COM:443",
			want:   "build-cloud.docker.com:443",
		},
		{
			name:    "Empty",
			rawURL:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := endpointHost(tt.rawURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFactoryNewEndpointNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		driverOpts   map[string]string
		wantProxy    string
		wantRegistry string
		wantHealth   string
	}{
		{
			name: "InternalAddressTCP",
			driverOpts: map[string]string{
				optKeyInternalAddress: "tcp://build-cloud.docker.com:443",
			},
			wantProxy:    "tcp://build-cloud.docker.com:443",
			wantRegistry: "build-cloud.docker.com:443",
			wantHealth:   "https://build-cloud.docker.com:443/v2/",
		},
		{
			name: "InternalAddressHTTPS",
			driverOpts: map[string]string{
				optKeyInternalAddress: "https://build-cloud.docker.com:443",
			},
			wantProxy:    "tcp://build-cloud.docker.com:443",
			wantRegistry: "build-cloud.docker.com:443",
			wantHealth:   "https://build-cloud.docker.com:443/v2/",
		},
		{
			name: "InternalAddressBare",
			driverOpts: map[string]string{
				optKeyInternalAddress: "build-cloud.docker.com:443",
			},
			wantProxy:    "tcp://build-cloud.docker.com:443",
			wantRegistry: "build-cloud.docker.com:443",
			wantHealth:   "https://build-cloud.docker.com:443/v2/",
		},
		{
			name: "InternalAddressLocalhostBare",
			driverOpts: map[string]string{
				optKeyInternalAddress: "localhost:5000",
			},
			wantProxy:    "tcp://localhost:5000",
			wantRegistry: "localhost:5000",
			wantHealth:   "https://localhost:5000/v2/",
		},
		{
			name: "InternalProxyAddressHTTPS",
			driverOpts: map[string]string{
				optKeyInternalProxyAddress: "https://build-cloud.docker.com:443",
			},
			wantProxy:    "tcp://build-cloud.docker.com:443",
			wantRegistry: RegistryAddress,
			wantHealth:   HealthAddress,
		},
		{
			name: "InternalProxyAddressBare",
			driverOpts: map[string]string{
				optKeyInternalProxyAddress: "build-cloud.docker.com:443",
			},
			wantProxy:    "tcp://build-cloud.docker.com:443",
			wantRegistry: RegistryAddress,
			wantHealth:   HealthAddress,
		},
		{
			name: "InternalProxyAddressIPv6",
			driverOpts: map[string]string{
				optKeyInternalProxyAddress: "[::1]:5000",
			},
			wantProxy:    "tcp://[::1]:5000",
			wantRegistry: RegistryAddress,
			wantHealth:   HealthAddress,
		},
		{
			name: "InternalProxyAddressMixedCase",
			driverOpts: map[string]string{
				optKeyInternalProxyAddress: "tcp://BUILD-CLOUD.DOCKER.COM:443",
			},
			wantProxy:    "tcp://build-cloud.docker.com:443",
			wantRegistry: RegistryAddress,
			wantHealth:   HealthAddress,
		},
		{
			name: "InternalRegistryAddress",
			driverOpts: map[string]string{
				optKeyInternalRegistryAddress: "https://BUILD-CLOUD.DOCKER.COM:443",
			},
			wantProxy:    ProxyAddress,
			wantRegistry: "build-cloud.docker.com:443",
			wantHealth:   HealthAddress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, err := (&factory{}).New(t.Context(), driver.InitConfig{
				EndpointAddr: "org/builder",
				DriverOpts:   tt.driverOpts,
			})
			require.NoError(t, err)

			cd, ok := d.(*Driver)
			require.True(t, ok)
			assert.Equal(t, tt.wantProxy, cd.proxyAddress)
			assert.Equal(t, tt.wantRegistry, cd.registryAddress)
			assert.Equal(t, tt.wantHealth, cd.healthAddress)
		})
	}
}

func TestTokenRefresh(t *testing.T) {
	t.Parallel()

	now := time.Now()
	count := 0
	s := func(context.Context) (string, error) {
		count++
		return fmt.Sprintf("token-%d", count), nil
	}
	nextToken := newTokenSource(s, func() time.Time { return now })

	token, err := nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)

	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)

	now = now.Add(10 * time.Minute)
	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-2", token)
}

func TestTokenRefreshInitialFailureRetries(t *testing.T) {
	t.Parallel()

	now := time.Now()
	expectedErr := errors.New("refresh failed")
	count := 0
	nextToken := newTokenSource(func(context.Context) (string, error) {
		count++
		if count == 1 {
			return "", expectedErr
		}
		return "token", nil
	}, func() time.Time { return now })

	token, err := nextToken(t.Context())
	require.ErrorIs(t, err, expectedErr)
	assert.Empty(t, token)

	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token", token)
	assert.Equal(t, 2, count)
}

func TestTokenRefreshFailureDoesNotCacheError(t *testing.T) {
	t.Parallel()

	now := time.Now()
	expectedErr := errors.New("refresh failed")
	count := 0
	nextToken := newTokenSource(func(context.Context) (string, error) {
		count++
		switch count {
		case 1:
			return "token-1", nil
		case 2:
			return "", expectedErr
		default:
			return "token-3", nil
		}
	}, func() time.Time { return now })

	token, err := nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-1", token)

	now = now.Add(10 * time.Minute)
	token, err = nextToken(t.Context())
	require.ErrorIs(t, err, expectedErr)
	assert.Empty(t, token)

	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token-3", token)
	assert.Equal(t, 3, count)
}

func TestTokenRefreshCancellationRetries(t *testing.T) {
	t.Parallel()

	now := time.Now()
	count := 0
	nextToken := newTokenSource(func(ctx context.Context) (string, error) {
		count++
		if err := context.Cause(ctx); err != nil {
			return "", err
		}
		return "token", nil
	}, func() time.Time { return now })

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(context.Canceled)
	token, err := nextToken(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, token)

	token, err = nextToken(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "token", token)
	assert.Equal(t, 2, count)
}

func TestGetUserContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		env      []string
	}{
		{
			name:     "empty env",
			env:      []string{},
			expected: "local",
		},
		{
			name:     "CI env var must be set or fallback to local",
			env:      []string{"CIRCLECI=true"},
			expected: "local",
		},
		{
			name:     "unknown CI",
			env:      []string{"CI=true"},
			expected: "ci",
		},
		{
			name:     "github ci detection",
			env:      []string{"CI=true", "GITHUB_ACTIONS=true"},
			expected: "github",
		},
		{
			name:     "gitlab ci detection",
			env:      []string{"CI=true", "GITLAB_CI=true"},
			expected: "gitlab",
		},
		{
			name:     "circleci ci detection",
			env:      []string{"CI=true", "CIRCLECI=true"},
			expected: "circleci",
		},
		{
			name:     "travis ci detection",
			env:      []string{"CI=true", "TRAVIS=true"},
			expected: "travis",
		},
		{
			name:     "buildkite ci detection",
			env:      []string{"CI=true", "BUILDKITE=true"},
			expected: "buildkite",
		},
		{
			name:     "jenkins ci detection",
			env:      []string{"CI=true", "JENKINS_HOME=/root/jenkins"},
			expected: "jenkins",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			userContext := getUserContext(tt.env)
			assert.Equal(t, tt.expected, userContext)
		})
	}
}
