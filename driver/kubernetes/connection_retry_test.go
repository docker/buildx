package kubernetes

import (
	"context"
	"crypto/x509"
	stderrors "errors"
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

type retryPods struct {
	client rest.Interface
	pods   []corev1.Pod
}

func (p *retryPods) RESTClient() rest.Interface { return p.client }
func (p *retryPods) List(context.Context, metav1.ListOptions) (*corev1.PodList, error) {
	return &corev1.PodList{Items: append([]corev1.Pod(nil), p.pods...)}, nil
}

func TestDialReselectsAfterStreamSetupFailure(t *testing.T) {
	for _, mode := range []string{"random", "sticky"} {
		for _, setupError := range []struct{ name, message string }{
			{name: "closed-pipe", message: "io: read/write on closed pipe"},
			{name: "tls-internal", message: "remote error: tls: internal error"},
		} {
			t.Run(mode+"/"+setupError.name, func(t *testing.T) {
				var mu sync.Mutex
				var paths []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					paths = append(paths, r.URL.Path)
					attempt := len(paths)
					mu.Unlock()
					if attempt == 1 {
						http.Error(w, setupError.message, http.StatusServiceUnavailable)
						return
					}
					http.Error(w, "forbidden by test policy", http.StatusForbidden)
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
					pods.pods = append(pods.pods, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "buildkitd"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}})
				}
				var chooser podchooser.PodChooser = &podchooser.RandomPodChooser{PodClient: pods}
				if mode == "sticky" {
					chooser = &podchooser.StickyPodChooser{Key: "same-build", PodClient: pods}
				}
				d := &Driver{clientConfig: &mockClientConfig{clientConfig: cfg}, podClient: pods, podChooser: chooser}
				ctx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Second, errors.New("exec retry test timed out"))
				defer cancel()
				conn, err := d.Dial(ctx)
				require.ErrorContains(t, err, "forbidden")
				require.Nil(t, conn)
				mu.Lock()
				defer mu.Unlock()
				require.Len(t, paths, 2, "authorization errors must not be retried")
				require.NotEqual(t, paths[0], paths[1], "retry must select another eligible worker")
			})
		}
	}
}

func TestConnectionRetryClassification(t *testing.T) {
	for _, err := range []error{
		io.ErrClosedPipe,
		io.ErrUnexpectedEOF,
		errors.Wrap(io.EOF, "wrapped"),
		errors.New("unable to upgrade connection: io: read/write on closed pipe"),
		errors.New("failed to write client preface: connection lost"),
		errors.New("error reading server preface: EOF"),
	} {
		require.True(t, isTransientConnectionError(err), err.Error())
	}
	for _, err := range []error{context.Canceled, errors.New("forbidden"), errors.New("x509: certificate signed by unknown authority"), errors.New("Dockerfile process exited 42")} {
		require.False(t, isTransientConnectionError(err), err.Error())
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.Canceled)
	calls := 0
	err := tryWithBackoff(ctx, "worker", func() error { calls++; return io.ErrClosedPipe })
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls)
}

func TestConnectionRetryPermanentErrorsTakePrecedence(t *testing.T) {
	for _, permanent := range []error{
		context.Canceled,
		&apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonForbidden, Code: http.StatusForbidden, Message: "EOF"}},
		&apierrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonUnauthorized, Code: http.StatusUnauthorized, Message: "EOF"}},
		x509.UnknownAuthorityError{},
		errors.New("Forbidden"),
		errors.New("Unauthorized"),
		errors.New("permission denied"),
		errors.New("tls: failed to verify certificate: verification failed"),
		errors.New("tls: bad certificate"),
		errors.New("tls: certificate required"),
	} {
		for _, prefix := range []string{"failed to write client preface", "error reading server preface", "unable to upgrade connection: io: read/write on closed pipe"} {
			err := errors.Wrap(permanent, prefix)
			require.False(t, isTransientConnectionError(err), err.Error())
			calls := 0
			got := tryWithBackoff(context.Background(), "worker", func() error { calls++; return err })
			require.ErrorIs(t, got, permanent)
			require.Equal(t, 1, calls, "a permanent setup error must not be retried")
		}
		// Error chains may carry an EOF as well as a definitive permanent error.
		err := stderrors.Join(io.EOF, permanent)
		require.False(t, isTransientConnectionError(err), err.Error())
	}
}

func TestRetryPodSelection(t *testing.T) {
	for _, mode := range []string{"random", "sticky"} {
		t.Run(mode, func(t *testing.T) {
			client := &retryPods{}
			for _, name := range []string{"worker-a", "worker-b", "worker-c"} {
				client.pods = append(client.pods, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}})
			}
			var chooser podchooser.PodChooser = &podchooser.RandomPodChooser{PodClient: client}
			if mode == "sticky" {
				chooser = &podchooser.StickyPodChooser{Key: "same-build", PodClient: client}
			}
			d := &Driver{podClient: client, podChooser: chooser}
			var attempted []string
			for range len(client.pods) {
				pod, err := d.chooseRetryPod(context.Background(), attempted)
				require.NoError(t, err)
				require.NotContains(t, attempted, pod.Name, "use every untried worker before falling back")
				attempted = append(attempted, pod.Name)
			}
			pod, err := d.chooseRetryPod(context.Background(), attempted)
			require.NoError(t, err)
			require.Contains(t, attempted, pod.Name, "all tried: retain bounded retries")
			client.pods = client.pods[:1]
			pod, err = d.chooseRetryPod(context.Background(), []string{client.pods[0].Name})
			require.NoError(t, err)
			require.Equal(t, client.pods[0].Name, pod.Name, "single worker remains retryable")
			client.pods[0].DeletionTimestamp = &metav1.Time{Time: time.Now()}
			pod, err = d.chooseRetryPod(context.Background(), attempted)
			require.Error(t, err)
			require.Nil(t, pod, "exhausted candidates must not restore a terminating worker")
		})
	}
}
