package podchooser

import (
	"cmp"
	"context"
	"crypto/md5" // #nosec G501 -- required for compatibility with existing sticky pod assignments
	"encoding/binary"
	"math/rand"
	"slices"

	"github.com/docker/buildx/driver/kubernetes/kubeclient"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodChooser interface {
	ChoosePod(ctx context.Context) (*corev1.Pod, error)
}

type RandomPodChooser struct {
	PodClient   kubeclient.PodClient
	Deployment  *appsv1.Deployment
	StatefulSet *appsv1.StatefulSet
}

func (pc *RandomPodChooser) ChoosePod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := ListRunningPods(ctx, pc.PodClient, pc.Deployment, pc.StatefulSet)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, errors.New("no running buildkit pods found")
	}
	n := rand.Intn(len(pods)) // #nosec G404 -- no strong seeding required
	logrus.Debugf("RandomPodChooser.ChoosePod(): len(pods)=%d, n=%d", len(pods), n)
	return pods[n], nil
}

type StickyPodChooser struct {
	Key         string
	PodClient   kubeclient.PodClient
	Deployment  *appsv1.Deployment
	StatefulSet *appsv1.StatefulSet
}

func (pc *StickyPodChooser) ChoosePod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := ListRunningPods(ctx, pc.PodClient, pc.Deployment, pc.StatefulSet)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		logrus.Errorf("no pod found for key %q", pc.Key)
		rpc := &RandomPodChooser{
			PodClient:   pc.PodClient,
			Deployment:  pc.Deployment,
			StatefulSet: pc.StatefulSet,
		}
		return rpc.ChoosePod(ctx)
	}
	key := stickyHash(pc.Key)
	var first, chosen *corev1.Pod
	var firstHash, chosenHash [2]int64
	for _, pod := range pods {
		h := stickyHash(pod.Name + "-0")
		if first == nil || slices.Compare(h[:], firstHash[:]) < 0 {
			first, firstHash = pod, h
		}
		// Select the first ring position strictly after the key, wrapping to
		// the smallest position if there is no successor.
		if slices.Compare(key[:], h[:]) < 0 && (chosen == nil || slices.Compare(h[:], chosenHash[:]) < 0) {
			chosen, chosenHash = pod, h
		}
	}
	if chosen == nil {
		chosen = first
	}
	return chosen, nil
}

// stickyHash preserves serialx/hashring's default ordering: an MD5 digest
// interpreted as a pair of signed, little-endian integers. Each unweighted
// pod occupies one ring position, hashing its name with the suffix "-0".
func stickyHash(key string) [2]int64 {
	h := md5.Sum([]byte(key)) // #nosec G401 -- used for consistent hashing, not security
	return [2]int64{int64(binary.LittleEndian.Uint64(h[:8])), int64(binary.LittleEndian.Uint64(h[8:]))}
}

func ListRunningPods(ctx context.Context, client kubeclient.PodClient, depl *appsv1.Deployment, stat *appsv1.StatefulSet) ([]*corev1.Pod, error) {
	var labelSelector *metav1.LabelSelector
	if depl != nil {
		labelSelector = depl.Spec.Selector
	} else if stat != nil {
		labelSelector = stat.Spec.Selector
	}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}
	listOpts := metav1.ListOptions{
		LabelSelector: selector.String(),
	}
	podList, err := client.List(ctx, listOpts)
	if err != nil {
		return nil, err
	}
	var runningPods []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		// Skip terminating Pods: they may still be Running while buildkitd shuts down,
		// causing newly scheduled builds to be killed mid-flight.
		if pod.DeletionTimestamp != nil {
			logrus.Debugf("pod terminating, skipping: %q", pod.Name)
			continue
		}
		if pod.Status.Phase == corev1.PodRunning {
			logrus.Debugf("pod running: %q", pod.Name)
			runningPods = append(runningPods, pod)
		}
	}
	slices.SortFunc(runningPods, func(a, b *corev1.Pod) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return runningPods, nil
}
