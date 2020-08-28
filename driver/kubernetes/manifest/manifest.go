package manifest

import (
	"strings"

	"github.com/docker/buildx/util/platformutil"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentOpt struct {
	Namespace     string
	Name          string
	Image         string
	Replicas      int
	BuildkitFlags []string
	Rootless      bool
	NodeSelector  map[string]string
	Platforms     []v1.Platform
}

const (
	containerName      = "buildkitd"
	AnnotationPlatform = "buildx.docker.com/platform"
)

func NewDeployment(opt *DeploymentOpt) (*appsv1.Deployment, error) {
	labels := map[string]string{
		"app": opt.Name,
	}
	annotations := map[string]string{}
	replicas := int32(opt.Replicas)
	privileged := true
	args := opt.BuildkitFlags

	if len(opt.Platforms) > 0 {
		annotations[AnnotationPlatform] = strings.Join(platformutil.Format(opt.Platforms), ",")
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   opt.Namespace,
			Name:        opt.Name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  containerName,
							Image: opt.Image,
							Args:  args,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{"buildctl", "debug", "workers"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	if opt.Rootless {
		if err := toRootless(d); err != nil {
			return nil, err
		}
	}

	if len(opt.NodeSelector) > 0 {
		d.Spec.Template.Spec.NodeSelector = opt.NodeSelector
	}

	return d, nil
}

func toRootless(d *appsv1.Deployment) error {
	d.Spec.Template.Spec.Containers[0].Args = append(
		d.Spec.Template.Spec.Containers[0].Args,
		"--oci-worker-no-process-sandbox",
	)
	d.Spec.Template.Spec.Containers[0].SecurityContext = nil
	if d.Spec.Template.ObjectMeta.Annotations == nil {
		d.Spec.Template.ObjectMeta.Annotations = make(map[string]string, 2)
	}
	d.Spec.Template.ObjectMeta.Annotations["container.apparmor.security.beta.kubernetes.io/"+containerName] = "unconfined"
	d.Spec.Template.ObjectMeta.Annotations["container.seccomp.security.alpha.kubernetes.io/"+containerName] = "unconfined"
	return nil
}
