package build

import (
	"testing"

	"github.com/docker/buildx/util/buildflags"
	"github.com/moby/buildkit/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #nosec G101 -- ignoring: Potential hardcoded credentials
func TestCacheOptions_DerivedVars(t *testing.T) {
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "sensitive_token")
	t.Setenv("ACTIONS_CACHE_URL", "https://cache.github.com")
	t.Setenv("AWS_ACCESS_KEY_ID", "definitely_dont_look_here")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "hackers_please_dont_steal")
	t.Setenv("AWS_SESSION_TOKEN", "not_a_mitm_attack")

	cacheFrom, err := buildflags.ParseCacheEntry([]string{"type=gha", "type=s3,region=us-west-2,bucket=my_bucket,name=my_image"})
	require.NoError(t, err)
	require.Equal(t, []client.CacheOptionsEntry{
		{
			Type: "gha",
			Attrs: map[string]string{
				"token": "sensitive_token",
				"url":   "https://cache.github.com",
			},
		},
		{
			Type: "s3",
			Attrs: map[string]string{
				"region":            "us-west-2",
				"bucket":            "my_bucket",
				"name":              "my_image",
				"access_key_id":     "definitely_dont_look_here",
				"secret_access_key": "hackers_please_dont_steal",
				"session_token":     "not_a_mitm_attack",
			},
		},
	}, CreateCaches(cacheFrom))
}

func TestCreateExports_RegistryUnpack(t *testing.T) {
	tests := []struct {
		name       string
		entries    []*buildflags.ExportEntry
		wantType   string
		wantPush   string
		wantUnpack string
	}{
		{
			name: "registry type sets unpack=false",
			entries: []*buildflags.ExportEntry{
				{
					Type:  "registry",
					Attrs: map[string]string{},
				},
			},
			wantType:   "image",
			wantPush:   "true",
			wantUnpack: "false",
		},
		{
			name: "registry type respects explicit unpack=true",
			entries: []*buildflags.ExportEntry{
				{
					Type: "registry",
					Attrs: map[string]string{
						"unpack": "true",
					},
				},
			},
			wantType:   "image",
			wantPush:   "true",
			wantUnpack: "true",
		},
		{
			name: "registry type respects explicit unpack=false",
			entries: []*buildflags.ExportEntry{
				{
					Type: "registry",
					Attrs: map[string]string{
						"unpack": "false",
					},
				},
			},
			wantType:   "image",
			wantPush:   "true",
			wantUnpack: "false",
		},
		{
			name: "image type without push does not set unpack",
			entries: []*buildflags.ExportEntry{
				{
					Type:  "image",
					Attrs: map[string]string{},
				},
			},
			wantType:   "image",
			wantPush:   "",
			wantUnpack: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exports, _, err := CreateExports(tt.entries)
			require.NoError(t, err)
			require.Len(t, exports, 1)

			require.Equal(t, tt.wantType, exports[0].Type)
			require.Equal(t, tt.wantPush, exports[0].Attrs["push"])
			require.Equal(t, tt.wantUnpack, exports[0].Attrs["unpack"])
		})
	}
}

func TestProxyArgKeyExists(t *testing.T) {
	tests := []struct {
		name      string
		proxyArgs map[string]string
		key       string
		want      bool
	}{
		{
			name:      "exact match",
			proxyArgs: map[string]string{"NO_PROXY": "cli"},
			key:       "NO_PROXY",
			want:      true,
		},
		{
			name:      "case insensitive match",
			proxyArgs: map[string]string{"no_proxy": "cli"},
			key:       "NO_PROXY",
			want:      true,
		},
		{
			name:      "no match",
			proxyArgs: map[string]string{"HTTP_PROXY": "cli"},
			key:       "NO_PROXY",
			want:      false,
		},
		{
			name:      "nil map",
			proxyArgs: nil,
			key:       "NO_PROXY",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, proxyArgKeyExists(tt.proxyArgs, tt.key))
		})
	}
}
