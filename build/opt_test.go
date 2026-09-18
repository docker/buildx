package build

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	noderesolver "github.com/docker/buildx/build/resolver"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/ocilayout"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	contextstore "github.com/docker/cli/cli/context/store"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/ociindex"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/pb"
	"github.com/moby/buildkit/util/apicaps"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type exporterTestDriver struct {
	driver.Driver
	moby     bool
	features func(context.Context) (map[driver.Feature]bool, error)
}

func (d exporterTestDriver) Info(context.Context) (*driver.Info, error) {
	return &driver.Info{Status: driver.Running}, nil
}

func (d exporterTestDriver) Client(context.Context, ...client.ClientOpt) (*client.Client, error) {
	return nil, nil
}

func (d exporterTestDriver) IsMobyDriver() bool {
	return d.moby
}

func (d exporterTestDriver) Features(ctx context.Context) (map[driver.Feature]bool, error) {
	if d.features != nil {
		return d.features(ctx)
	}
	return map[driver.Feature]bool{driver.DockerExporter: true}, nil
}

type exporterTestCLI struct {
	command.Cli
	store          contextstore.Store
	currentContext string
}

func (c exporterTestCLI) ContextStore() contextstore.Store {
	return c.store
}

func (c exporterTestCLI) CurrentContext() string {
	return c.currentContext
}

func TestDockerExporterFeatureProbe(t *testing.T) {
	var goodCalls, badCalls atomic.Int32
	newServer := func(available bool, calls *atomic.Int32) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/_ping") {
				w.Header().Set("API-Version", "1.55")
				return
			}
			if !strings.HasSuffix(r.URL.Path, "/info") {
				http.NotFound(w, r)
				return
			}
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if !available {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"message":"daemon unavailable"}`)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(server.Close)
		return server
	}
	good := newServer(true, &goodCalls)
	bad := newServer(false, &badCalls)
	for _, tt := range []struct {
		name           string
		moby           bool
		endpoint       string
		currentContext string
		exportContext  string
		tarball        bool
		wantError      bool
	}{
		{name: "docker uses builder", moby: true, endpoint: good.URL, currentContext: bad.URL},
		{name: "docker ignores output context", moby: true, endpoint: good.URL, currentContext: bad.URL, exportContext: bad.URL},
		{name: "docker reports builder failure", moby: true, endpoint: bad.URL, currentContext: good.URL, wantError: true},
		{name: "remote uses output context", endpoint: bad.URL, currentContext: bad.URL, exportContext: good.URL},
		{name: "remote reports current context failure", endpoint: good.URL, currentContext: bad.URL, wantError: true},
		{name: "tarball skips daemon probe", endpoint: bad.URL, currentContext: bad.URL, tarball: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			goodCalls.Store(0)
			badCalls.Store(0)
			nodes, err := noderesolver.Resolve(t.Context(), []builder.Node{{
				Node:   store.Node{Endpoint: tt.endpoint},
				Driver: &driver.DriverHandle{Driver: exporterTestDriver{moby: tt.moby}},
			}}, nil, nil)
			require.NoError(t, err)
			require.Len(t, nodes, 1)
			cli := exporterTestCLI{
				store:          contextstore.New(t.TempDir(), command.DefaultContextStoreConfig()),
				currentContext: tt.currentContext,
			}
			export := client.ExportEntry{Type: "docker", Attrs: map[string]string{"context": tt.exportContext}}
			if tt.tarball {
				export.Output = func(map[string]string) (io.WriteCloser, error) { return nil, nil }
			}
			opt := &Options{
				Inputs:  Inputs{ContextPath: "https://example.com/context.tar.gz"},
				Exports: []client.ExportEntry{export},
				Policy:  []buildflags.PolicyConfig{{Disabled: true}},
			}
			cfg := confutil.NewConfig(nil, confutil.WithDir(t.TempDir()))
			so, release, err := toSolveOpt(t.Context(), nodes[0], false, opt, gateway.BuildOpts{}, cfg, testProgressWriter{}, dockerutil.NewClient(cli))
			if tt.wantError {
				require.ErrorContains(t, err, "failed to detect docker features")
				require.ErrorContains(t, err, "daemon unavailable")
				require.EqualValues(t, 1, badCalls.Load())
				require.Zero(t, goodCalls.Load())
				return
			}
			require.NoError(t, err)
			defer release(nil)
			require.Zero(t, badCalls.Load())
			if tt.tarball {
				require.Zero(t, goodCalls.Load())
			} else {
				require.EqualValues(t, 1, goodCalls.Load())
			}
			if tt.moby {
				require.Equal(t, "moby", so.Exports[0].Type)
			} else {
				require.Equal(t, "docker", so.Exports[0].Type)
			}
		})
	}
}

func TestDriverFeatureFailurePreservesProvenance(t *testing.T) {
	t.Setenv(noDefaultAttestationsEnv, "false")
	probeErr := errors.New("worker is starting")
	calls := 0
	d := exporterTestDriver{
		moby: true,
		features: func(context.Context) (map[driver.Feature]bool, error) {
			calls++
			if calls == 1 {
				return nil, probeErr
			}
			return map[driver.Feature]bool{driver.MultiPlatform: true}, nil
		},
	}
	nodes, err := noderesolver.Resolve(t.Context(), []builder.Node{{
		Driver: &driver.DriverHandle{Driver: d},
	}}, nil, nil)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	opt := &Options{
		Inputs:    Inputs{ContextPath: "https://example.com/context.tar.gz"},
		Exports:   []client.ExportEntry{{Type: "image", Attrs: map[string]string{}}},
		Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"}},
		Policy:    []buildflags.PolicyConfig{{Disabled: true}},
	}
	cfg := confutil.NewConfig(nil, confutil.WithDir(t.TempDir()))
	bopts := buildOptsWithCaps(apicaps.CapID("exporter.image.attestations"))
	so, release, err := toSolveOpt(t.Context(), nodes[0], false, opt, bopts, cfg, testProgressWriter{}, nil)
	require.ErrorIs(t, err, probeErr)
	require.ErrorContains(t, err, "failed to detect driver features")
	require.Nil(t, so)
	require.Nil(t, release)
	require.Equal(t, 1, calls)

	so, release, err = toSolveOpt(t.Context(), nodes[0], false, opt, bopts, cfg, testProgressWriter{}, nil)
	require.NoError(t, err)
	defer release(nil)
	require.Equal(t, "mode=min,inline-only=true", so.FrontendAttrs["attest:provenance"])
	require.Equal(t, 2, calls)
}

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

func TestApplyPolicyCapsEnablesProxyNetwork(t *testing.T) {
	p := policyWithDecision(`
package docker

decision := {
	"allow": false,
	"caps": {"exec.proxy": true},
}
`)
	var so client.SolveOpt

	err := applyPolicyCaps(context.Background(), p, buildOptsWithCaps(pb.CapExecMetaNetworkProxy), &so)
	require.NoError(t, err)
	require.True(t, so.ProxyNetwork)
}

func TestApplyPolicyCapsOrsProxyNetwork(t *testing.T) {
	falsePolicy := policyWithDecision(`
package docker

decision := {
	"allow": true,
	"caps": {"exec.proxy": false},
}
`)
	truePolicy := policyWithDecision(`
package docker

decision := {
	"allow": true,
	"caps": {"exec.proxy": true},
}
`)
	var so client.SolveOpt

	err := applyPolicyCaps(context.Background(), falsePolicy, buildOptsWithCaps(pb.CapExecMetaNetworkProxy), &so)
	require.NoError(t, err)
	require.False(t, so.ProxyNetwork)

	err = applyPolicyCaps(context.Background(), truePolicy, buildOptsWithCaps(pb.CapExecMetaNetworkProxy), &so)
	require.NoError(t, err)
	require.True(t, so.ProxyNetwork)
}

func TestApplyPolicyCapsRequiresBuildKitCap(t *testing.T) {
	p := policyWithDecision(`
package docker

decision := {
	"allow": true,
	"caps": {"exec.proxy": true},
}
`)
	var so client.SolveOpt

	err := applyPolicyCaps(context.Background(), p, buildOptsWithCaps(), &so)
	require.ErrorContains(t, err, "network proxy requested by policy is not supported by the current BuildKit daemon")
	require.ErrorContains(t, err, "please upgrade to version v0.31+")
	require.False(t, so.ProxyNetwork)
}

func TestLoadInputsOCILayoutNamedContext(t *testing.T) {
	layoutPath := t.TempDir()

	idx := ociindex.NewStoreIndex(layoutPath)
	manifestDigest := digest.FromString("manifest")
	err := idx.Put(ocispecs.Descriptor{
		MediaType: ocispecs.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      1,
	}, ociindex.Tag("latest"))
	require.NoError(t, err)

	tests := []struct {
		name    string
		ref     string
		wantRef ocilayout.Ref
	}{
		{
			name: "digest only",
			ref:  "oci-layout://" + layoutPath + "@" + manifestDigest.String(),
			wantRef: ocilayout.Ref{
				Digest: manifestDigest,
			},
		},
		{
			name: "tag only",
			ref:  "oci-layout://" + layoutPath + ":latest",
			wantRef: ocilayout.Ref{
				Tag:    "latest",
				Digest: manifestDigest,
			},
		},
		{
			name: "tag and digest",
			ref:  "oci-layout://" + layoutPath + ":latest@" + manifestDigest.String(),
			wantRef: ocilayout.Ref{
				Tag:    "latest",
				Digest: manifestDigest,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &client.SolveOpt{
				FrontendAttrs: map[string]string{},
			}
			inp := &Inputs{
				ContextPath: "https://example.com/context.tar.gz",
				NamedContexts: map[string]NamedContext{
					"proxy": {
						Path: tt.ref,
					},
				},
			}

			release, err := loadInputs(context.Background(), nil, inp, testProgressWriter{}, target)
			require.NoError(t, err)
			require.NotNil(t, release)
			t.Cleanup(release)

			attr, ok := target.FrontendAttrs["context:proxy"]
			require.True(t, ok)
			require.Len(t, target.OCIStores, 1)

			parsed, ok, err := ocilayout.Parse(attr)
			require.True(t, ok)
			require.NoError(t, err)
			require.NotEmpty(t, parsed.Path)
			assert.Equal(t, tt.wantRef.Tag, parsed.Tag)
			assert.Equal(t, tt.wantRef.Digest, parsed.Digest)
			target.OCIStores = nil
		})
	}
}

func TestPolicyProgressLoggerCloseWithErrorAfterCompletedWindow(t *testing.T) {
	pw := &captureProgressWriter{}
	logger := newPolicyProgressLogger(pw, "loading policies policy.rego")

	logger.Log("policy decision: DENY")
	logger.completeWindow(1, nil)
	logger.Close(errors.New("source not allowed by policy"))

	var completedErrors []string
	for _, st := range pw.Statuses() {
		for _, v := range st.Vertexes {
			if v == nil || v.Completed == nil {
				continue
			}
			if v.Error == "" {
				continue
			}
			completedErrors = append(completedErrors, v.Error)
		}
	}

	require.Equal(t, []string{"source not allowed by policy"}, completedErrors)
}

type testProgressWriter struct{}

func (testProgressWriter) Write(*client.SolveStatus) {}

func (testProgressWriter) WriteBuildRef(string, string) {}

func (testProgressWriter) ValidateLogSource(digest.Digest, any) bool { return true }

func (testProgressWriter) ClearLogSource(any) {}

var _ progress.Writer = testProgressWriter{}

type captureProgressWriter struct {
	mu       sync.Mutex
	statuses []*client.SolveStatus
}

func (w *captureProgressWriter) Write(st *client.SolveStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.statuses = append(w.statuses, st)
}

func (w *captureProgressWriter) Statuses() []*client.SolveStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*client.SolveStatus(nil), w.statuses...)
}

func (w *captureProgressWriter) WriteBuildRef(string, string) {}

func (w *captureProgressWriter) ValidateLogSource(digest.Digest, any) bool { return true }

func (w *captureProgressWriter) ClearLogSource(any) {}

var _ progress.Writer = (*captureProgressWriter)(nil)

func policyWithDecision(decision string) *policy.Policy {
	return policy.NewPolicy(policy.Opt{
		Files: []policy.File{{
			Filename: "policy.rego",
			Data:     []byte(decision),
		}},
	})
}

func buildOptsWithCaps(caps ...apicaps.CapID) gateway.BuildOpts {
	out := make([]*apicaps.PBCap, 0, len(caps))
	for _, c := range caps {
		out = append(out, &apicaps.PBCap{
			ID:      string(c),
			Enabled: true,
		})
	}
	return gateway.BuildOpts{
		LLBCaps: pb.Caps.CapSet(out),
	}
}
