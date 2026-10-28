package dockerconfig

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/buildx/util/confutil"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/moby/buildkit/session/auth"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/stretchr/testify/require"
)

// The test binary doubles as a credential helper so the tests exercise the
// Docker CLI's subprocess protocol on every supported platform.
func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "docker-credential-buildx-test" {
		if len(os.Args) != 2 || os.Args[1] != "get" {
			os.Exit(1)
		}
		host, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(1)
		}
		dir := os.Getenv("BUILDX_TEST_CREDENTIAL_HELPER")
		calls, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(1)
		}
		_, err = fmt.Fprintln(calls, strings.TrimSpace(string(host)))
		closeErr := calls.Close()
		if err != nil || closeErr != nil {
			os.Exit(1)
		}
		response, err := os.ReadFile(filepath.Join(dir, "response"))
		if err != nil {
			fmt.Fprintln(os.Stdout, "test credential helper unavailable")
			os.Exit(1)
		}
		if _, err := os.Stdout.Write(response); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAuthConfigCredentialHelperRefresh(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	binary, err := os.ReadFile(executable)
	require.NoError(t, err)
	helper := "docker-credential-buildx-test"
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, helper), binary, 0o700)) //nolint:gosec // The credential helper must be executable.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, method := range []string{http.MethodPost, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			stateDir := t.TempDir()
			t.Setenv("BUILDX_TEST_CREDENTIAL_HELPER", stateDir)
			const host = "registry.example.com"
			const username = "_dcgcloud_token"
			const scope = "repository:project/image:pull,push"
			cfg := configfile.New(filepath.Join(stateDir, "config.json"))
			cfg.CredentialHelpers = map[string]string{host: "buildx-test"}
			ap := &authConfigProvider{
				defaultConfig:   cfg,
				buildxConfig:    confutil.NewConfig(nil, confutil.WithDir(stateDir)),
				authConfigCache: map[string]authConfigCacheEntry{},
			}
			provider := authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
				AuthConfigProvider: ap.load,
			}).(auth.AuthServer)

			var acceptedSecret atomic.Value
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if method == http.MethodGet && r.Method == http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				if r.Method != method || r.ParseForm() != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				user, secret := r.Form.Get("username"), r.Form.Get("password")
				if r.Method == http.MethodGet {
					user, secret, _ = r.BasicAuth()
				}
				if user != username || secret != acceptedSecret.Load().(string) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Form.Get("service") != host || r.Form.Get("scope") != scope {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "registry-" + secret,
					"expires_in":   60,
				})
			}))
			defer server.Close()

			// Rotate the helper's credentials independently of the provider cache.
			// Aging the cache avoids sleeping for the default 4m50s lifetime.
			steps := []struct {
				name        string
				secret      string
				expire      bool
				helperFail  bool
				credentials bool
				wantSecret  string
				wantCalls   int
			}{
				{
					name:       "initial credentials",
					secret:     "token-1",
					wantSecret: "token-1",
					wantCalls:  1,
				},
				{
					name:       "reuse cached credentials",
					secret:     "token-2",
					wantSecret: "token-1",
					wantCalls:  1,
				},
				{
					name:       "refresh expired credentials",
					secret:     "token-2",
					expire:     true,
					wantSecret: "token-2",
					wantCalls:  2,
				},
				{
					name:       "propagate helper failure",
					expire:     true,
					helperFail: true,
					wantCalls:  3,
				},
				{
					name:       "retry failed refresh",
					secret:     "token-3",
					wantSecret: "token-3",
					wantCalls:  4,
				},
				{
					name:        "refresh via credentials RPC",
					secret:      "token-4",
					expire:      true,
					credentials: true,
					wantSecret:  "token-4",
					wantCalls:   5,
				},
			}
			for _, step := range steps {
				if step.helperFail {
					require.NoError(t, os.Remove(filepath.Join(stateDir, "response")))
				} else {
					response, err := json.Marshal(map[string]string{
						"ServerURL": host,
						"Username":  username,
						"Secret":    step.secret,
					})
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(filepath.Join(stateDir, "response"), response, 0o600))
				}
				if step.expire {
					entry := ap.authConfigCache[host]
					entry.Created = time.Now().Add(-5 * time.Minute)
					ap.authConfigCache[host] = entry
				}
				acceptedSecret.Store(step.wantSecret)
				before := requests.Load()
				if step.credentials {
					creds, err := provider.Credentials(t.Context(), &auth.CredentialsRequest{Host: host})
					require.NoError(t, err, step.name)
					require.Equal(t, username, creds.Username, step.name)
					require.Equal(t, step.wantSecret, creds.Secret, step.name)
				}
				resp, err := provider.FetchToken(t.Context(), &auth.FetchTokenRequest{
					Host:    host,
					Realm:   server.URL,
					Service: host,
					Scopes:  []string{scope},
				})
				if step.helperFail {
					require.ErrorContains(t, err, "test credential helper unavailable", step.name)
					require.Nil(t, resp, step.name)
					require.Equal(t, before, requests.Load(), step.name)
				} else {
					require.NoError(t, err, step.name)
					require.Equal(t, "registry-"+step.wantSecret, resp.Token, step.name)
				}
				calls, err := os.ReadFile(filepath.Join(stateDir, "calls"))
				require.NoError(t, err)
				require.Equal(t, strings.Repeat(host+"\n", step.wantCalls), string(calls), step.name)
			}
		})
	}
}

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
