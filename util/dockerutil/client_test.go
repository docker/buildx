package dockerutil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/store"
	"github.com/moby/moby/api/types/system"
	"github.com/stretchr/testify/require"
)

type featureTestCLI struct {
	command.Cli
	store          store.Store
	currentContext string
}

func (c featureTestCLI) ContextStore() store.Store {
	return c.store
}

func (c featureTestCLI) CurrentContext() string {
	return c.currentContext
}

func newFeatureTestServer(t *testing.T, info http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.55")
		case strings.HasSuffix(r.URL.Path, "/info"):
			w.Header().Set("Content-Type", "application/json")
			info(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFeaturesRetry(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsupported", true: "supported"}[supported], func(t *testing.T) {
			var calls atomic.Int32
			server := newFeatureTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"message":"daemon is starting"}`))
					return
				}
				info := system.Info{}
				if supported {
					info.DriverStatus = [][2]string{{"driver-type", "io.containerd.snapshotter.v1"}}
				}
				_ = json.NewEncoder(w).Encode(info)
			})
			c := NewClient(featureTestCLI{store: store.New(t.TempDir(), command.DefaultContextStoreConfig())})
			ctx := t.Context()
			features, err := c.Features(ctx, server.URL)
			require.ErrorContains(t, err, "daemon is starting")
			require.Nil(t, features)
			for range 2 {
				features, err = c.Features(ctx, server.URL)
				require.NoError(t, err)
				require.Equal(t, supported, features[OCIImporter])
			}
			require.EqualValues(t, 2, calls.Load())
		})
	}
}

func TestFeaturesContexts(t *testing.T) {
	var supportedCalls, unsupportedCalls atomic.Int32
	supported := newFeatureTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		supportedCalls.Add(1)
		_ = json.NewEncoder(w).Encode(system.Info{DriverStatus: [][2]string{{"driver-type", "io.containerd.snapshotter.v1"}}})
	})
	unsupported := newFeatureTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		unsupportedCalls.Add(1)
		_ = json.NewEncoder(w).Encode(system.Info{})
	})
	c := NewClient(featureTestCLI{
		store:          store.New(t.TempDir(), command.DefaultContextStoreConfig()),
		currentContext: supported.URL,
	})
	for range 2 {
		for _, name := range []string{"", unsupported.URL, supported.URL} {
			features, err := c.Features(t.Context(), name)
			require.NoError(t, err)
			require.Equal(t, name != unsupported.URL, features[OCIImporter])
		}
	}
	require.EqualValues(t, 1, supportedCalls.Load())
	require.EqualValues(t, 1, unsupportedCalls.Load())
}

func TestFeaturesCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := newFeatureTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		_ = json.NewEncoder(w).Encode(system.Info{DriverStatus: [][2]string{{"driver-type", "io.containerd.snapshotter.v1"}}})
	})
	t.Cleanup(func() { close(release) })
	c := NewClient(featureTestCLI{store: store.New(t.TempDir(), command.DefaultContextStoreConfig())})
	ctx, cancel := context.WithTimeoutCause(t.Context(), 10*time.Second, context.DeadlineExceeded)
	defer cancel()
	probeCtx, cancelProbe := context.WithCancelCause(ctx)
	defer cancelProbe(context.Canceled)
	done := make(chan error, 1)
	go func() {
		_, err := c.Features(probeCtx, server.URL)
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	cancelProbe(context.Canceled)
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-ctx.Done():
		t.Fatal("probe did not stop after cancellation")
	}
	features, err := c.Features(ctx, server.URL)
	require.NoError(t, err)
	require.True(t, features[OCIImporter])
	require.EqualValues(t, 2, calls.Load())
}
