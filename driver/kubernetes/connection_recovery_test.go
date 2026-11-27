package kubernetes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/docker/buildx/driver/kubernetes/kubeclient"
	"github.com/docker/buildx/driver/kubernetes/podchooser"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	protocol "k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/rest"
	"k8s.io/streaming/pkg/httpstream"
	"k8s.io/streaming/pkg/httpstream/spdy"
)

// A real HTTP exec request fails before upgrade, then the next worker completes
// a real SPDY upgrade and delivers bytes. This tests Dial, not solve replay.
func TestDialRecoversOnAnotherWorker(t *testing.T) {
	for _, mode := range []string{"random", "sticky"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			serverErrors := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				attempt := len(paths)
				mu.Unlock()
				if attempt == 1 {
					http.Error(w, "io: read/write on closed pipe", http.StatusServiceUnavailable)
					return
				}
				if _, err := httpstream.Handshake(r, w, []string{protocol.StreamProtocolV4Name}); err != nil {
					serverErrors <- err
					return
				}
				streams := make(chan httpstream.Stream, 4)
				wire := spdy.NewResponseUpgrader().UpgradeResponse(w, r, func(s httpstream.Stream, _ <-chan struct{}) error { streams <- s; return nil })
				if wire == nil {
					return
				}
				defer wire.Close()
				byType := map[string]httpstream.Stream{}
				timer := time.NewTimer(5 * time.Second)
				defer timer.Stop()
				for len(byType) < 4 {
					select {
					case s := <-streams:
						byType[s.Headers().Get(corev1.StreamType)] = s
					case <-wire.CloseChan():
						return
					case <-timer.C:
						serverErrors <- context.DeadlineExceeded
						return
					}
				}
				if _, err := byType[corev1.StreamTypeStdout].Write([]byte("RECOVERED")); err != nil {
					serverErrors <- err
					return
				}
				<-wire.CloseChan()
			}))
			defer server.Close()
			cfg := &rest.Config{
				Host:    server.URL,
				APIPath: "/api",
				ContentConfig: rest.ContentConfig{
					GroupVersion:         &schema.GroupVersion{Version: "v1"},
					NegotiatedSerializer: kubeclient.Codecs().WithoutConversion(),
				},
			}
			client, err := rest.RESTClientFor(cfg)
			require.NoError(t, err)
			pods := &retryPods{client: client}
			for _, name := range []string{"worker-a", "worker-b"} {
				pods.pods = append(pods.pods, corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "buildkitd"}}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
				})
			}
			var chooser podchooser.PodChooser = &podchooser.RandomPodChooser{PodClient: pods}
			if mode == "sticky" {
				chooser = &podchooser.StickyPodChooser{Key: "same-build", PodClient: pods}
			}
			d := &Driver{clientConfig: &mockClientConfig{clientConfig: cfg}, podClient: pods, podChooser: chooser}
			ctx, cancel := context.WithTimeoutCause(context.Background(), 10*time.Second, errors.New("exec recovery test timed out"))
			defer cancel()
			conn, err := d.Dial(ctx)
			require.NoError(t, err, "connection setup must recover on an alternate worker")
			defer conn.Close()
			payload := make([]byte, len("RECOVERED"))
			_, err = io.ReadFull(conn, payload)
			require.NoError(t, err, "Dial must not return a connection whose setup already failed")
			require.Equal(t, "RECOVERED", string(payload))
			mu.Lock()
			observed := append([]string(nil), paths...)
			mu.Unlock()
			require.Len(t, observed, 2)
			require.NotEqual(t, observed[0], observed[1], "second attempt must use another worker")
			t.Logf("attempt 1: %s -> HTTP 503 / io: read/write on closed pipe", observed[0])
			t.Logf("attempt 2: %s -> SPDY upgrade succeeded; payload=%s", observed[1], payload)
			require.NoError(t, conn.Close())
			require.NoError(t, context.Cause(ctx), "connection cleanup must preserve the parent context")
			select {
			case err := <-serverErrors:
				t.Fatal(err)
			default:
			}
		})
	}
}

func TestSetupRetryIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 20*time.Second, errors.New("setup retry test timed out"))
	defer cancel()
	attempts := 0
	err := tryWithBackoff(ctx, "test-worker", func() error { attempts++; return io.ErrClosedPipe })
	require.ErrorContains(t, err, "after 5 attempts")
	require.Equal(t, 5, attempts)
	t.Logf("persistent closed pipe: stopped after %d attempts: %v", attempts, err)
}

func TestSetupRetryStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.Canceled)
	attempts := 0
	err := tryWithBackoff(ctx, "test-worker", func() error { attempts++; cancel(context.Canceled); return io.ErrClosedPipe })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
	t.Logf("cancellation during first failure: stopped after %d attempt: %v", attempts, err)
}
