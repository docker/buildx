package podchooser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestChoosersRequireReadiness(t *testing.T) {
	for _, controller := range []struct {
		name        string
		deployment  *appsv1.Deployment
		statefulSet *appsv1.StatefulSet
	}{
		{name: "deployment", deployment: newDeployment()},
		{name: "statefulset", statefulSet: &appsv1.StatefulSet{Spec: appsv1.StatefulSetSpec{Selector: newDeployment().Spec.Selector}}},
	} {
		t.Run(controller.name, func(t *testing.T) {
			for _, mode := range []string{"random", "sticky"} {
				t.Run(mode, func(t *testing.T) {
					for _, condition := range []struct {
						name   string
						status corev1.ConditionStatus
					}{
						{name: "false", status: corev1.ConditionFalse},
						{name: "unknown", status: corev1.ConditionUnknown},
						{name: "missing"},
					} {
						t.Run(condition.name, func(t *testing.T) {
							ctx := context.Background()
							client := &fakePodClient{pods: []corev1.Pod{
								newPod("worker-a", corev1.PodRunning),
								newPod("worker-b", corev1.PodRunning),
							}}
							var chooser PodChooser = &RandomPodChooser{PodClient: client, Deployment: controller.deployment, StatefulSet: controller.statefulSet}
							if mode == "sticky" {
								chooser = &StickyPodChooser{Key: "same-build", PodClient: client, Deployment: controller.deployment, StatefulSet: controller.statefulSet}
							}
							selected, err := chooser.ChoosePod(ctx)
							require.NoError(t, err)
							selectedName := selected.Name
							// In particular, invalidate the worker the sticky key actually selected.
							for i := range client.pods {
								if client.pods[i].Name == selectedName {
									client.pods[i].Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}}
									if condition.status != "" {
										client.pods[i].Status.Conditions = append(client.pods[i].Status.Conditions, corev1.PodCondition{Type: corev1.PodReady, Status: condition.status})
									}
								}
							}
							for range 20 {
								selected, err = chooser.ChoosePod(ctx)
								require.NoError(t, err)
								require.NotEqual(t, selectedName, selected.Name, "an unready worker must not remain eligible")
							}
							// Never fall back to an unready worker when no eligible one remains.
							client.pods = []corev1.Pod{newPod(selectedName, corev1.PodRunning)}
							client.pods[0].Status.Conditions = nil
							_, err = chooser.ChoosePod(ctx)
							require.EqualError(t, err, "no ready buildkit pods found")
							// Restored readiness makes the same worker eligible again.
							client.pods[0] = newPod(selectedName, corev1.PodRunning)
							selected, err = chooser.ChoosePod(ctx)
							require.NoError(t, err)
							require.Equal(t, selectedName, selected.Name)
						})
					}
				})
			}
		})
	}
}
