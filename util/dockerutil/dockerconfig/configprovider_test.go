package dockerconfig

import (
	"testing"

	"context"
	"time"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/stretchr/testify/require"
)

func TestParseConfigKey(t *testing.T) {
	tests := map[string]struct {
		in    string
		host  string
		repo  string
		scope map[string]struct{}
	}{
		"domain-only": {
			in:   "example.com",
			host: "example.com",
		},
		"domain-with-path": {
			in:   "example.com/a/b",
			host: "example.com",
			repo: "a/b",
		},
		"with-scopes": {
			in:   "example.com/x@y,z",
			host: "example.com",
			repo: "x",
			scope: map[string]struct{}{
				"y": {},
				"z": {},
			},
		},
		"empty-scope-list": {
			in:   "example.com/a/b@",
			host: "example.com",
			repo: "a/b",
		},
		"path-empty-before-at": {
			in:   "example.com/@foo",
			host: "example.com",
			repo: "",
			scope: map[string]struct{}{
				"foo": {},
			},
		},
		"scope-no-slash": {
			in:   "registry-1.docker.io@pull,push",
			host: "registry-1.docker.io",
			repo: "",
			scope: map[string]struct{}{
				"pull": {},
				"push": {},
			},
		},
		"with-port-and-scopes": {
			in:   "example.com:5000/x@y,z",
			host: "example.com:5000",
			repo: "x",
			scope: map[string]struct{}{
				"y": {},
				"z": {},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			out := parseConfigKey(tc.in)

			require.Equal(t, tc.host, out.host)
			require.Equal(t, tc.repo, out.repo)

			if tc.scope == nil {
				require.Nil(t, out.scope)
			} else {
				require.Equal(t, tc.scope, out.scope)
			}
		})
	}
}
func TestMatchesScopes(t *testing.T) {
	tests := map[string]struct {
		cfgKey  string
		query   []string
		matches bool
	}{
		"invalid-scope": {
			cfgKey:  "example.com/foo/bar@a,b",
			query:   []string{"repository:foo/bar:pull"},
			matches: false,
		},
		"sub-scope-match": {
			cfgKey:  "example.com/foo/bar@push",
			query:   []string{"repository:foo/bar:pull,push"},
			matches: true,
		},
		"invalid-scope-single": {
			cfgKey:  "example.com/foo/bar@push",
			query:   []string{"repository:foo/bar:pull"},
			matches: false,
		},
		"wrong-repo": {
			cfgKey:  "example.com/foo/bar@push",
			query:   []string{"repository:foo/bar2:pull,push"},
			matches: false,
		},
		"multi-scope-match": {
			cfgKey:  "example.com/foo/bar@push,pull",
			query:   []string{"repository:foo/bar:pull,push"},
			matches: true,
		},
		"empty-action-list-in-query": {
			cfgKey:  "example.com/foo/bar@pull",
			query:   []string{"repository:foo/bar:"}, // no actions
			matches: false,
		},

		"repo-no-scopes-required": {
			cfgKey:  "example.com/foo/bar",
			query:   []string{"repository:foo/bar:pull"},
			matches: true,
		},

		"repo-empty-scope-in-config": {
			cfgKey:  "example.com/foo/bar@", // explicit empty scope list
			query:   []string{"repository:foo/bar:pull"},
			matches: true,
		},

		"multiple-repos-config-targets-only-one": {
			cfgKey: "example.com/foo/bar@pull",
			query: []string{
				"repository:foo/bar:pull",
				"repository:other:push",
			},
			matches: true,
		},

		"multiple-repos-but-target-lacks-scope": {
			cfgKey: "example.com/foo/bar@push",
			query: []string{
				"repository:foo/bar:pull", // missing push
				"repository:other:push",   // irrelevant
			},
			matches: false,
		},

		"no-repo-in-config-match-any": {
			cfgKey: "example.com@pull",
			query: []string{
				"repository:a:pull",
				"repository:b:pull,push",
			},
			matches: true,
		},

		"no-repo-in-config-but-no-repo-has-scope": {
			cfgKey: "example.com@push",
			query: []string{
				"repository:a:pull",
				"repository:b:pull",
			},
			matches: false,
		},

		"single-action-in-query-with-spaces": {
			cfgKey:  "example.com/foo/bar@pull",
			query:   []string{"repository:foo/bar:pull repository:foo/bar2:pull"},
			matches: true,
		},

		"single-action-in-query-with-spaces-one-match": {
			cfgKey:  "example.com@push",
			query:   []string{"repository:foo/bar:push repository:foo/bar2:pull"},
			matches: true,
		},

		"single-action-in-query-with-spaces-invalid": {
			cfgKey:  "example.com@push",
			query:   []string{"repository:foo/bar:pull repository:foo/bar2:pull"},
			matches: false,
		},

		"multi-scope-config-order-does-not-matter": {
			cfgKey:  "example.com/foo/bar@push,pull",
			query:   []string{"repository:foo/bar:pull,push"},
			matches: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := parseConfigKey(tc.cfgKey)
			q := parseScopes(tc.query)
			got := cfg.matchesScopes(q)
			require.Equal(t, tc.matches, got)
		})
	}
}

func TestAuthConfigCache_Hit(t *testing.T) {
	cfg := configfile.New("test")
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-1",
	}

	ap := &authConfigProvider{
		defaultConfig:   cfg,
		authConfigCache: map[string]authConfigCacheEntry{},
	}
	ap.initOnce.Do(func() {})

	ctx := context.Background()
	ac, err := ap.load(ctx, "europe-west1-docker.pkg.dev", nil, func(created time.Time, serverURL string) bool {
		return false
	})
	require.NoError(t, err)
	require.Equal(t, "token-1", ac.Password)

	// Update config in underlying store
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-2",
	}

	// Should still hit cache and return token-1
	ac, err = ap.load(ctx, "europe-west1-docker.pkg.dev", nil, func(created time.Time, serverURL string) bool {
		return false
	})
	require.NoError(t, err)
	require.Equal(t, "token-1", ac.Password)
}

func TestAuthConfigCache_Expired(t *testing.T) {
	cfg := configfile.New("test")
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-1",
	}

	ap := &authConfigProvider{
		defaultConfig:   cfg,
		authConfigCache: map[string]authConfigCacheEntry{},
	}
	ap.initOnce.Do(func() {})

	ctx := context.Background()
	ac, err := ap.load(ctx, "europe-west1-docker.pkg.dev", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "token-1", ac.Password)

	// Update config in underlying store
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-2",
	}

	// Force expire check to return true
	ac, err = ap.load(ctx, "europe-west1-docker.pkg.dev", nil, func(created time.Time, serverURL string) bool {
		return true
	})
	require.NoError(t, err)
	require.Equal(t, "token-2", ac.Password)
}

func TestAuthConfigCache_DefaultExpireCheck_Nil(t *testing.T) {
	cfg := configfile.New("test")
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-1",
	}

	ap := &authConfigProvider{
		defaultConfig:   cfg,
		authConfigCache: map[string]authConfigCacheEntry{},
	}
	ap.initOnce.Do(func() {})

	ctx := context.Background()
	// Initial load
	ac, err := ap.load(ctx, "europe-west1-docker.pkg.dev", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "token-1", ac.Password)

	// Change underlying config
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-2",
	}

	// Recent cache entry should not expire with nil check
	ac, err = ap.load(ctx, "europe-west1-docker.pkg.dev", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "token-1", ac.Password)

	// Age the cached entry past 4m50s
	entry := ap.authConfigCache["europe-west1-docker.pkg.dev"]
	entry.Created = time.Now().Add(-5 * time.Minute)
	ap.authConfigCache["europe-west1-docker.pkg.dev"] = entry

	// Should expire with default check and reload token-2
	ac, err = ap.load(ctx, "europe-west1-docker.pkg.dev", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "token-2", ac.Password)
}

func TestAuthConfigCache_LoadHost_DirectNilCheck(t *testing.T) {
	cfg := configfile.New("test")
	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-1",
	}

	ap := &authConfigProvider{
		defaultConfig:   cfg,
		authConfigCache: map[string]authConfigCacheEntry{},
	}
	ap.initOnce.Do(func() {})

	ctx := context.Background()
	// Direct call to loadHost with nil cacheExpireCheck
	ac, err := ap.loadHost(ctx, "europe-west1-docker.pkg.dev", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "token-1", ac.Password)

	cfg.AuthConfigs["europe-west1-docker.pkg.dev"] = types.AuthConfig{
		Username: "_dcgcloud_token",
		Password: "token-2",
	}

	// Age the entry
	entry := ap.authConfigCache["europe-west1-docker.pkg.dev"]
	entry.Created = time.Now().Add(-5 * time.Minute)
	ap.authConfigCache["europe-west1-docker.pkg.dev"] = entry

	// loadHost directly with nil should expire and reload
	ac, err = ap.loadHost(ctx, "europe-west1-docker.pkg.dev", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "token-2", ac.Password)
}
