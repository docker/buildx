package manifest

import (
	"fmt"
	"path"
	"strings"

	"github.com/docker/buildx/util/platformutil"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type StatefulSetOpt struct {
	Namespace          string
	Name               string
	Image              string
	Replicas           int
	ServiceAccountName string
	SchedulerName      string

	// Qemu
	Qemu struct {
		// when true, will install binfmt
		Install bool
		Image   string
	}

	BuildkitFlags []string
	// files mounted at /etc/buildkitd
	ConfigFiles map[string][]byte

	Rootless                  bool
	NodeSelector              map[string]string
	CustomAnnotations         map[string]string
	CustomLabels              map[string]string
	Tolerations               []corev1.Toleration
	RequestsCPU               string
	RequestsMemory            string
	RequestsPersistentStorage resource.Quantity
	LimitsCPU                 string
	LimitsMemory              string
	LimitsPersistentStorage   string
	Platforms                 []v1.Platform
}

const (
	containerName            = "buildkitd"
	AnnotationPlatform       = "buildx.docker.com/platform"
	LabelApp                 = "app"
	ProbeFailureThreshold    = 3
	ProbeInitialDelaySeconds = 5
	ProbePeriodSeconds       = 30
	ProbeSuccessThreshold    = 1
	ProbeTimeoutSeconds      = 60
)

var ProbeHandlerCommand = []string{"buildctl", "debug", "workers"}

type ErrReservedAnnotationPlatform struct{}

func (ErrReservedAnnotationPlatform) Error() string {
	return fmt.Sprintf("the annotation %q is reserved and cannot be customized", AnnotationPlatform)
}

type ErrReservedLabelApp struct{}

func (ErrReservedLabelApp) Error() string {
	return fmt.Sprintf("the label %q is reserved and cannot be customized", LabelApp)
}

func NewStatefulSet(opt *StatefulSetOpt) (s *appsv1.StatefulSet, c []*corev1.ConfigMap, err error) {
	labels := map[string]string{
		LabelApp: opt.Name,
	}
	annotations := map[string]string{}
	replicas := int32(opt.Replicas)
	privileged := true
	args := opt.BuildkitFlags

	if len(opt.Platforms) > 0 {
		annotations[AnnotationPlatform] = strings.Join(platformutil.Format(opt.Platforms), ",")
	}

	for k, v := range opt.CustomAnnotations {
		if k == AnnotationPlatform {
			return nil, nil, ErrReservedAnnotationPlatform{}
		}
		annotations[k] = v
	}

	for k, v := range opt.CustomLabels {
		if k == LabelApp {
			return nil, nil, ErrReservedLabelApp{}
		}
		labels[k] = v
	}

	const persistentVolumeClaimName = "buildkitd"
	s = &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   opt.Namespace,
			Name:        opt.Name,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName:         "buildkitd",
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Replicas:            &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: opt.ServiceAccountName,
					SchedulerName:      opt.SchedulerName,
					Containers: []corev1.Container{
						{
							Name:  containerName,
							Image: opt.Image,
							Args:  args,
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: ProbeHandlerCommand,
									},
								},
								FailureThreshold:    ProbeFailureThreshold,
								InitialDelaySeconds: ProbeInitialDelaySeconds,
								PeriodSeconds:       ProbePeriodSeconds,
								SuccessThreshold:    ProbeSuccessThreshold,
								TimeoutSeconds:      ProbeTimeoutSeconds,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									Exec: &corev1.ExecAction{
										Command: ProbeHandlerCommand,
									},
								},
								FailureThreshold:    ProbeFailureThreshold,
								InitialDelaySeconds: ProbeInitialDelaySeconds,
								PeriodSeconds:       ProbePeriodSeconds,
								SuccessThreshold:    ProbeSuccessThreshold,
								TimeoutSeconds:      ProbeTimeoutSeconds,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{},
								Limits:   corev1.ResourceList{},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      persistentVolumeClaimName,
									ReadOnly:  false,
									MountPath: "/var/lib/buildkit",
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: persistentVolumeClaimName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: opt.RequestsPersistentStorage,
							},
						},
					},
				},
			},
		},
	}
	for _, cfg := range splitConfigFiles(opt.ConfigFiles) {
		cc := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   opt.Namespace,
				Name:        opt.Name + "-" + cfg.name,
				Annotations: annotations,
			},
			Data: cfg.files,
		}

		s.Spec.Template.Spec.Containers[0].VolumeMounts = append(s.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      cfg.name,
			MountPath: path.Join("/etc/buildkit", cfg.path),
		})

		s.Spec.Template.Spec.Volumes = append(s.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: cfg.name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cc.Name,
					},
				},
			},
		})
		c = append(c, cc)
	}

	if opt.Qemu.Install {
		s.Spec.Template.Spec.InitContainers = []corev1.Container{
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
		if err := toRootless(s); err != nil {
			return nil, nil, err
		}
	}

	if len(opt.NodeSelector) > 0 {
		s.Spec.Template.Spec.NodeSelector = opt.NodeSelector
	}

	if len(opt.Tolerations) > 0 {
		s.Spec.Template.Spec.Tolerations = opt.Tolerations
	}

	if opt.RequestsCPU != "" {
		reqCPU, err := resource.ParseQuantity(opt.RequestsCPU)
		if err != nil {
			return nil, nil, err
		}
		s.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = reqCPU
	}

	if opt.RequestsMemory != "" {
		reqMemory, err := resource.ParseQuantity(opt.RequestsMemory)
		if err != nil {
			return nil, nil, err
		}
		s.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = reqMemory
	}

	if opt.LimitsCPU != "" {
		limCPU, err := resource.ParseQuantity(opt.LimitsCPU)
		if err != nil {
			return nil, nil, err
		}
		s.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = limCPU
	}

	if opt.LimitsMemory != "" {
		limMemory, err := resource.ParseQuantity(opt.LimitsMemory)
		if err != nil {
			return nil, nil, err
		}
		s.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = limMemory
	}

	if opt.LimitsPersistentStorage != "" {
		limPersistentStorage, err := resource.ParseQuantity(opt.LimitsPersistentStorage)
		if err != nil {
			return nil, nil, err
		}
		s.Spec.VolumeClaimTemplates[0].Spec.Resources.Limits = corev1.ResourceList{
			corev1.ResourceStorage: limPersistentStorage,
		}
	}

	return
}

func toRootless(s *appsv1.StatefulSet) error {
	s.Spec.Template.Spec.Containers[0].Args = append(
		s.Spec.Template.Spec.Containers[0].Args,
		"--oci-worker-no-process-sandbox",
	)
	s.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeUnconfined,
		},
	}
	if s.Spec.Template.ObjectMeta.Annotations == nil {
		s.Spec.Template.ObjectMeta.Annotations = make(map[string]string, 1)
	}
	s.Spec.Template.ObjectMeta.Annotations["container.apparmor.security.beta.kubernetes.io/"+containerName] = "unconfined"

	// Dockerfile has `VOLUME /home/user/.local/share/buildkit` by default too,
	// but the default VOLUME does not work with rootless on Google's Container-Optimized OS
	// as it is mounted with `nosuid,nodev`.
	// https://github.com/moby/buildkit/issues/879#issuecomment-1240347038
	// https://github.com/moby/buildkit/pull/3097
	s.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath = "/home/user/.local/share/buildkit"
	return nil
}

type config struct {
	name  string
	path  string
	files map[string]string
}

func splitConfigFiles(m map[string][]byte) []config {
	var c []config
	idx := map[string]int{}
	nameIdx := 0
	for k, v := range m {
		dir := path.Dir(k)
		i, ok := idx[dir]
		if !ok {
			idx[dir] = len(c)
			i = len(c)
			name := "config"
			if dir != "." {
				nameIdx++
				name = fmt.Sprintf("%s-%d", name, nameIdx)
			}
			c = append(c, config{
				path:  dir,
				name:  name,
				files: map[string]string{},
			})
		}
		c[i].files[path.Base(k)] = string(v)
	}
	return c
}
