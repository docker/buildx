package podchooser

import (
	"context"
	"math/rand"
	"sort"
	"time"

	"github.com/serialx/hashring"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type PodChooser interface {
	ChoosePod(ctx context.Context) (*corev1.Pod, error)
}

type RandomPodChooser struct {
	RandSource rand.Source
	PodClient  clientcorev1.PodInterface
	Deployment *appsv1.Deployment
}

func (pc *RandomPodChooser) ChoosePod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := ListRunningPods(pc.PodClient, pc.Deployment)
	if err != nil {
		return nil, err
	}
	randSource := pc.RandSource
	if randSource == nil {
		randSource = rand.NewSource(time.Now().Unix())
	}
	rnd := rand.New(randSource)
	n := rnd.Int() % len(pods)
	logrus.Debugf("RandomPodChooser.ChoosePod(): len(pods)=%d, n=%d", len(pods), n)
	return pods[n], nil
}

type StickyPodChooser struct {
	Key        string
	PodClient  clientcorev1.PodInterface
	Deployment *appsv1.Deployment
}

func (pc *StickyPodChooser) ChoosePod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := ListRunningPods(pc.PodClient, pc.Deployment)
	if err != nil {
		return nil, err
	}
	var podNames []string
	podMap := make(map[string]*corev1.Pod, len(pods))
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
		podMap[pod.Name] = pod
	}
	ring := hashring.New(podNames)
	chosen, ok := ring.GetNode(pc.Key)
	if !ok {
		// NOTREACHED
		logrus.Errorf("no pod found for key %q", pc.Key)
		rpc := &RandomPodChooser{
			PodClient:  pc.PodClient,
			Deployment: pc.Deployment,
		}
		return rpc.ChoosePod(ctx)
	}
	return podMap[chosen], nil
}

func ListRunningPods(client clientcorev1.PodInterface, depl *appsv1.Deployment) ([]*corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(depl.Spec.Selector)
	if err != nil {
		return nil, err
	}
	listOpts := metav1.ListOptions{
		LabelSelector: selector.String(),
	}
	podList, err := client.List(listOpts)
	if err != nil {
		return nil, err
	}
	var runningPods []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			logrus.Debugf("pod runnning: %q", pod.Name)
			runningPods = append(runningPods, pod)
		}
	}
	sort.Slice(runningPods, func(i, j int) bool {
		return runningPods[i].Name < runningPods[j].Name
	})
	return runningPods, nil
}
