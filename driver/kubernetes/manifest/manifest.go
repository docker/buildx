package manifest

import (
	"strings"

	"github.com/docker/buildx/util/platformutil"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentOpt struct {
	Namespace string
	Name      string
	Image     string
	Replicas  int

	// Qemu
	Qemu struct {
		// when true, will install binfmt
		Install bool
		Image   string
	}

	BuildkitFlags []string
	// BuildkitConfig
	// when not empty, will create configmap with buildkit.toml and mounted
	BuildkitConfig []byte

	Rootless       bool
	NodeSelector   map[string]string
	RequestsCPU    string
	RequestsMemory string
	LimitsCPU      string
	LimitsMemory   string
	Platforms      []v1.Platform
}

const (
	containerName      = "buildkitd"
	AnnotationPlatform = "buildx.docker.com/platform"
)

func NewDeployment(opt *DeploymentOpt) (d *appsv1.Deployment, c *corev1.ConfigMap, err error) {
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

	d = &appsv1.Deployment{
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
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{},
								Limits:   corev1.ResourceList{},
							},
						},
					},
				},
			},
		},
	}

	if len(opt.BuildkitConfig) > 0 {
		c = &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   opt.Namespace,
				Name:        opt.Name + "-config",
				Annotations: annotations,
			},
			Data: map[string]string{
				"buildkitd.toml": string(opt.BuildkitConfig),
			},
		}

		d.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
			Name:      "config",
			MountPath: "/etc/buildkit",
		}}

		d.Spec.Template.Spec.Volumes = []corev1.Volume{{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c.Name,
					},
				},
			},
		}}
	}

	if opt.Qemu.Install {
		d.Spec.Template.Spec.InitContainers = []corev1.Container{
			{
				Name:  "qemu",
				Image: opt.Qemu.Image,
				Args:  []string{"--install", "all"},
				SecurityContext: &corev1.SecurityContext{
					Privileged: &privileged,
				},
			},
		}
	}

	if opt.Rootless {
		if err := toRootless(d); err != nil {
			return nil, nil, err
		}
	}

	if len(opt.NodeSelector) > 0 {
		d.Spec.Template.Spec.NodeSelector = opt.NodeSelector
	}

	if opt.RequestsCPU != "" {
		reqCPU, err := resource.ParseQuantity(opt.RequestsCPU)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = reqCPU
	}

	if opt.RequestsMemory != "" {
		reqMemory, err := resource.ParseQuantity(opt.RequestsMemory)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = reqMemory
	}

	if opt.LimitsCPU != "" {
		limCPU, err := resource.ParseQuantity(opt.LimitsCPU)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = limCPU
	}

	if opt.LimitsMemory != "" {
		limMemory, err := resource.ParseQuantity(opt.LimitsMemory)
		if err != nil {
			return nil, nil, err
		}
		d.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = limMemory
	}

	return
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
