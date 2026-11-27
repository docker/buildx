package kubernetes

import (
	"context"
	"math/rand/v2"
	"slices"

	"github.com/docker/buildx/driver/kubernetes/podchooser"
	corev1 "k8s.io/api/core/v1"
)

// chooseRetryPod preserves configured affinity on the first attempt. After a
// setup failure, prefer an untried eligible worker without changing upstream
// chooser APIs or hashing. Reuse the configured policy once all have been tried.
func (d *Driver) chooseRetryPod(ctx context.Context, attempted []string) (*corev1.Pod, error) {
	if len(attempted) == 0 {
		return d.podChooser.ChoosePod(ctx)
	}
	pods, err := podchooser.ListRunningPods(ctx, d.podClient, d.deployment, d.statefulSet)
	if err != nil {
		return nil, err
	}
	pods = slices.DeleteFunc(pods, func(pod *corev1.Pod) bool { return slices.Contains(attempted, pod.Name) })
	if len(pods) == 0 {
		return d.podChooser.ChoosePod(ctx)
	}
	return pods[rand.N(len(pods))], nil // #nosec G404 -- worker selection does not require cryptographic randomness
}
