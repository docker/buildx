package podchooser

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// fakePodClient is a fake kubeclient.PodClient returning a fixed pod list. It
// stands in for the real pod API so that the filtering in ListRunningPods can be
// tested without a Kubernetes API server. The label selector is not honored:
// callers pass exactly the pods they want the selector to have matched.
type fakePodClient struct {
	pods []corev1.Pod
	err  error
}

func (f *fakePodClient) List(_ context.Context, _ metav1.ListOptions) (*corev1.PodList, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &corev1.PodList{Items: f.pods}, nil
}

func (f *fakePodClient) RESTClient() rest.Interface {
	panic("unimplemented")
}

func newDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "buildkit", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "buildkit"},
			},
		},
	}
}

func newPod(name string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

// terminating marks a pod as deleted the way the API server does: the pod keeps
// its phase and its containers keep running until the grace period expires.
func terminating(pod corev1.Pod) corev1.Pod {
	now := metav1.NewTime(time.Now())
	pod.DeletionTimestamp = &now
	return pod
}

func podNames(pods []*corev1.Pod) []string {
	names := make([]string, 0, len(pods))
	for _, pod := range pods {
		names = append(names, pod.Name)
	}
	return names
}

func TestListRunningPods(t *testing.T) {
	t.Run("skips terminating pods", func(t *testing.T) {
		// A pod that has been marked for deletion stays in the Running phase for
		// the whole of its termination grace period, so the phase alone does not
		// tell us whether it is on its way out.
		client := &fakePodClient{pods: []corev1.Pod{
			newPod("pod-a", corev1.PodRunning),
			terminating(newPod("pod-b", corev1.PodRunning)),
			newPod("pod-c", corev1.PodRunning),
		}}

		pods, err := ListRunningPods(context.Background(), client, newDeployment(), nil)
		require.NoError(t, err)
		require.Equal(t, []string{"pod-a", "pod-c"}, podNames(pods))
	})

	t.Run("keeps the last pod that is not terminating", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			terminating(newPod("pod-a", corev1.PodRunning)),
			newPod("pod-b", corev1.PodRunning),
		}}

		pods, err := ListRunningPods(context.Background(), client, newDeployment(), nil)
		require.NoError(t, err)
		require.Equal(t, []string{"pod-b"}, podNames(pods))
	})

	t.Run("returns nothing when every pod is terminating", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			terminating(newPod("pod-a", corev1.PodRunning)),
			terminating(newPod("pod-b", corev1.PodRunning)),
		}}

		pods, err := ListRunningPods(context.Background(), client, newDeployment(), nil)
		require.NoError(t, err)
		require.Empty(t, pods)
	})

	t.Run("skips pods that are not running", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			newPod("pod-a", corev1.PodPending),
			newPod("pod-b", corev1.PodRunning),
			newPod("pod-c", corev1.PodSucceeded),
			newPod("pod-d", corev1.PodFailed),
		}}

		pods, err := ListRunningPods(context.Background(), client, newDeployment(), nil)
		require.NoError(t, err)
		require.Equal(t, []string{"pod-b"}, podNames(pods))
	})

	t.Run("sorts pods by name", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			newPod("pod-c", corev1.PodRunning),
			newPod("pod-a", corev1.PodRunning),
			newPod("pod-b", corev1.PodRunning),
		}}

		pods, err := ListRunningPods(context.Background(), client, newDeployment(), nil)
		require.NoError(t, err)
		require.Equal(t, []string{"pod-a", "pod-b", "pod-c"}, podNames(pods))
	})
}

func TestRandomPodChooserSkipsTerminatingPods(t *testing.T) {
	t.Run("never returns a terminating pod", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			terminating(newPod("pod-a", corev1.PodRunning)),
			newPod("pod-b", corev1.PodRunning),
			terminating(newPod("pod-c", corev1.PodRunning)),
		}}
		pc := &RandomPodChooser{PodClient: client, Deployment: newDeployment()}

		// The choice is random, so repeat: pod-b is the only valid candidate.
		for range 20 {
			pod, err := pc.ChoosePod(context.Background())
			require.NoError(t, err)
			require.Equal(t, "pod-b", pod.Name)
		}
	})

	t.Run("errors when every pod is terminating", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			terminating(newPod("pod-a", corev1.PodRunning)),
		}}
		pc := &RandomPodChooser{PodClient: client, Deployment: newDeployment()}

		_, err := pc.ChoosePod(context.Background())
		require.EqualError(t, err, "no running buildkit pods found")
	})
}

func TestStickyPodChooserSkipsTerminatingPods(t *testing.T) {
	t.Run("does not stick to a terminating pod", func(t *testing.T) {
		pods := []corev1.Pod{
			newPod("pod-a", corev1.PodRunning),
			newPod("pod-b", corev1.PodRunning),
			newPod("pod-c", corev1.PodRunning),
		}
		key := "some-context-path-hash"

		// Find the pod this key hashes to while all pods are healthy, then mark
		// exactly that pod terminating: the chooser must pick a different one.
		pc := &StickyPodChooser{Key: key, PodClient: &fakePodClient{pods: pods}, Deployment: newDeployment()}
		chosen, err := pc.ChoosePod(context.Background())
		require.NoError(t, err)

		withTerminating := make([]corev1.Pod, 0, len(pods))
		for _, pod := range pods {
			if pod.Name == chosen.Name {
				pod = terminating(pod)
			}
			withTerminating = append(withTerminating, pod)
		}

		pc = &StickyPodChooser{Key: key, PodClient: &fakePodClient{pods: withTerminating}, Deployment: newDeployment()}
		got, err := pc.ChoosePod(context.Background())
		require.NoError(t, err)
		require.NotEqual(t, chosen.Name, got.Name)
		require.Nil(t, got.DeletionTimestamp)
	})

	t.Run("errors when every pod is terminating", func(t *testing.T) {
		client := &fakePodClient{pods: []corev1.Pod{
			terminating(newPod("pod-a", corev1.PodRunning)),
			terminating(newPod("pod-b", corev1.PodRunning)),
		}}
		pc := &StickyPodChooser{Key: "key", PodClient: client, Deployment: newDeployment()}

		_, err := pc.ChoosePod(context.Background())
		require.EqualError(t, err, "no running buildkit pods found")
	})
}
