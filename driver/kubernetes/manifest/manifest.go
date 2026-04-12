package manifest

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/docker/buildx/util/platformutil"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	yaml "go.yaml.in/yaml/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentOpt struct {
	Namespace          string
	Name               string
	Image              string
	Replicas           int32
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

	BuildKitRootVolumeMemory  string
	Rootless                  bool
	NodeSelector              map[string]string
	CustomAnnotations         map[string]string
	CustomLabels              map[string]string
	Tolerations               []corev1.Toleration
	ManifestPatch             string
	RequestsCPU               string
	RequestsMemory            string
	RequestsEphemeralStorage  string
	RequestsPersistentStorage string
	LimitsCPU                 string
	LimitsMemory              string
	LimitsEphemeralStorage    string
	Platforms                 []ocispecs.Platform
	Env                       []corev1.EnvVar // injected into main buildkitd container
}

const (
	containerName             = "buildkitd"
	AnnotationPlatform        = "buildx.docker.com/platform"
	LabelApp                  = "app"
	rootVolumeName            = "buildkit-memory"
	rootVolumePath            = "/var/lib/buildkit"
	persistentVolumeClaimName = "buildkitd"

	probeFailureThreshold    = 3
	probeInitialDelaySeconds = 5
	probePeriodSeconds       = 30
	probeSuccessThreshold    = 1
	probeTimeoutSeconds      = 60
)

type ErrReservedAnnotationPlatform struct{}

func (ErrReservedAnnotationPlatform) Error() string {
	return fmt.Sprintf("the annotation %q is reserved and cannot be customized", AnnotationPlatform)
}

type ErrReservedLabelApp struct{}

func (ErrReservedLabelApp) Error() string {
	return fmt.Sprintf("the label %q is reserved and cannot be customized", LabelApp)
}

func NewDeployment(opt *DeploymentOpt) (d *appsv1.Deployment, s *appsv1.StatefulSet, c []*corev1.ConfigMap, err error) {
	labels := map[string]string{
		LabelApp: opt.Name,
	}
	annotations := map[string]string{}
	replicas := opt.Replicas
	privileged := true
	args := opt.BuildkitFlags

	if len(opt.Platforms) > 0 {
		annotations[AnnotationPlatform] = strings.Join(platformutil.Format(opt.Platforms), ",")
	}

	for k, v := range opt.CustomAnnotations {
		if k == AnnotationPlatform {
			return nil, nil, nil, ErrReservedAnnotationPlatform{}
		}
		annotations[k] = v
	}

	for k, v := range opt.CustomLabels {
		if k == LabelApp {
			return nil, nil, nil, ErrReservedLabelApp{}
		}
		labels[k] = v
	}

	probeHandlerCommand := []string{"buildctl", "debug", "workers"}
	podTemplate := corev1.PodTemplateSpec{
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
								Command: probeHandlerCommand,
							},
						},
						FailureThreshold:    probeFailureThreshold,
						InitialDelaySeconds: probeInitialDelaySeconds,
						PeriodSeconds:       probePeriodSeconds,
						SuccessThreshold:    probeSuccessThreshold,
						TimeoutSeconds:      probeTimeoutSeconds,
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							Exec: &corev1.ExecAction{
								Command: probeHandlerCommand,
							},
						},
						FailureThreshold:    probeFailureThreshold,
						InitialDelaySeconds: probeInitialDelaySeconds,
						PeriodSeconds:       probePeriodSeconds,
						SuccessThreshold:    probeSuccessThreshold,
						TimeoutSeconds:      probeTimeoutSeconds,
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{},
						Limits:   corev1.ResourceList{},
					},
				},
			},
		},
	}

	meta := metav1.ObjectMeta{
		Namespace:   opt.Namespace,
		Name:        opt.Name,
		Labels:      labels,
		Annotations: annotations,
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

		podTemplate.Spec.Containers[0].VolumeMounts = append(podTemplate.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      cfg.name,
			MountPath: path.Join("/etc/buildkit", cfg.path),
		})

		podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, corev1.Volume{
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
		podTemplate.Spec.InitContainers = []corev1.Container{
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
		if err := toRootless(&podTemplate); err != nil {
			return nil, nil, nil, err
		}
	}

	if len(opt.NodeSelector) > 0 {
		podTemplate.Spec.NodeSelector = opt.NodeSelector
	}

	if len(opt.Tolerations) > 0 {
		podTemplate.Spec.Tolerations = opt.Tolerations
	}

	if opt.RequestsCPU != "" {
		reqCPU, err := resource.ParseQuantity(opt.RequestsCPU)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = reqCPU
	}

	if opt.RequestsMemory != "" {
		reqMemory, err := resource.ParseQuantity(opt.RequestsMemory)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory] = reqMemory
	}

	if opt.RequestsEphemeralStorage != "" {
		reqStorage, err := resource.ParseQuantity(opt.RequestsEphemeralStorage)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Containers[0].Resources.Limits[corev1.ResourceEphemeralStorage] = reqStorage
	}

	if opt.LimitsCPU != "" {
		limCPU, err := resource.ParseQuantity(opt.LimitsCPU)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = limCPU
	}

	if opt.LimitsMemory != "" {
		limMemory, err := resource.ParseQuantity(opt.LimitsMemory)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = limMemory
	}

	if opt.LimitsEphemeralStorage != "" {
		limStorage, err := resource.ParseQuantity(opt.LimitsEphemeralStorage)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Containers[0].Resources.Limits[corev1.ResourceEphemeralStorage] = limStorage
	}

	if opt.BuildKitRootVolumeMemory != "" {
		buildKitRootVolumeMemory, err := resource.ParseQuantity(opt.BuildKitRootVolumeMemory)
		if err != nil {
			return nil, nil, nil, err
		}
		podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, corev1.Volume{
			Name: rootVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    "Memory",
					SizeLimit: &buildKitRootVolumeMemory,
				},
			},
		})
		podTemplate.Spec.Containers[0].VolumeMounts = append(podTemplate.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      rootVolumeName,
			MountPath: rootVolumePath,
		})
	}

	if len(opt.Env) > 0 {
		podTemplate.Spec.Containers[0].Env = append(podTemplate.Spec.Containers[0].Env, opt.Env...)
	}

	if opt.IsPersistentStorage() {
		s = &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "StatefulSet",
			},
			ObjectMeta: meta,
			Spec: appsv1.StatefulSetSpec{
				ServiceName:         "buildkitd",
				PodManagementPolicy: appsv1.ParallelPodManagement,
				Replicas:            &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: podTemplate,
			},
		}

		if err := setupPersistentStorage(s, opt); err != nil {
			return nil, nil, nil, err
		}
	} else {
		d = &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
			},
			ObjectMeta: meta,
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
				Template: podTemplate,
			},
		}
	}

	if opt.ManifestPatch != "" {
		if d != nil {
			if err := applyManifestPatch(d, opt.ManifestPatch); err != nil {
				return nil, nil, nil, err
			}
		}
		if s != nil {
			if err := applyManifestPatch(s, opt.ManifestPatch); err != nil {
				return nil, nil, nil, err
			}
		}
		for _, cfgMap := range c {
			if err := applyManifestPatch(cfgMap, opt.ManifestPatch); err != nil {
				return nil, nil, nil, err
			}
		}
	}

	return
}

func applyManifestPatch(obj any, patch string) error {
	path, value, err := parseManifestPatch(patch)
	if err != nil {
		return err
	}

	dt, err := json.Marshal(obj)
	if err != nil {
		return errors.Wrap(err, "marshal manifest")
	}

	var doc map[string]any
	if err := json.Unmarshal(dt, &doc); err != nil {
		return errors.Wrap(err, "unmarshal manifest")
	}

	if err := setManifestPatchValue(doc, path, value); err != nil {
		return err
	}

	dt, err = json.Marshal(doc)
	if err != nil {
		return errors.Wrap(err, "marshal patched manifest")
	}
	if err := json.Unmarshal(dt, obj); err != nil {
		return errors.Wrap(err, "unmarshal patched manifest")
	}
	return nil
}

func parseManifestPatch(patch string) ([]string, any, error) {
	lhs, rhs, ok := strings.Cut(patch, "=")
	if !ok {
		return nil, nil, errors.Errorf("invalid manifest-patch %q: expected assignment expression", patch)
	}

	lhs = strings.TrimSpace(lhs)
	rhs = strings.TrimSpace(rhs)
	if lhs == "" || rhs == "" {
		return nil, nil, errors.Errorf("invalid manifest-patch %q: expected non-empty assignment", patch)
	}
	if !strings.HasPrefix(lhs, ".") {
		return nil, nil, errors.Errorf("invalid manifest-patch %q: path must start with '.'", patch)
	}

	path := strings.Split(strings.TrimPrefix(lhs, "."), ".")
	for _, segment := range path {
		if segment == "" {
			return nil, nil, errors.Errorf("invalid manifest-patch %q: empty path segment", patch)
		}
	}

	var value any
	if err := yaml.Unmarshal([]byte(rhs), &value); err != nil {
		return nil, nil, errors.Wrapf(err, "invalid manifest-patch %q", patch)
	}
	return path, value, nil
}

func setManifestPatchValue(doc map[string]any, path []string, value any) error {
	current := doc
	for i, segment := range path {
		if i == len(path)-1 {
			current[segment] = value
			return nil
		}
		next, ok := current[segment]
		if !ok {
			child := map[string]any{}
			current[segment] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return errors.Errorf("invalid manifest-patch path %q: %q is not an object", strings.Join(path, "."), strings.Join(path[:i+1], "."))
		}
		current = child
	}
	return nil
}

func (opt *DeploymentOpt) IsPersistentStorage() bool {
	return opt.RequestsPersistentStorage != ""
}

func setupPersistentStorage(s *appsv1.StatefulSet, opt *DeploymentOpt) error {
	reqStorage, err := resource.ParseQuantity(opt.RequestsPersistentStorage)
	if err != nil {
		return err
	}

	// Rootless already sets up the data mount.
	if !opt.Rootless {
		s.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				Name:      persistentVolumeClaimName,
				ReadOnly:  false,
				MountPath: "/var/lib/buildkit",
			},
		}
	}

	s.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: persistentVolumeClaimName,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: reqStorage,
					},
				},
			},
		},
	}
	return nil
}

func toRootless(p *corev1.PodTemplateSpec) error {
	p.Spec.Containers[0].Args = append(
		p.Spec.Containers[0].Args,
		"--oci-worker-no-process-sandbox",
	)
	p.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeUnconfined,
		},
	}
	if p.Annotations == nil {
		p.Annotations = make(map[string]string, 1)
	}
	p.Annotations["container.apparmor.security.beta.kubernetes.io/"+containerName] = "unconfined"

	// Dockerfile has `VOLUME /home/user/.local/share/buildkit` by default too,
	// but the default VOLUME does not work with rootless on Google's Container-Optimized OS
	// as it is mounted with `nosuid,nodev`.
	// https://github.com/moby/buildkit/issues/879#issuecomment-1240347038
	// https://github.com/moby/buildkit/pull/3097
	const emptyDirVolName = persistentVolumeClaimName
	p.Spec.Containers[0].VolumeMounts = append(p.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name:      emptyDirVolName,
		MountPath: "/home/user/.local/share/buildkit",
	})
	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{
		Name: emptyDirVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

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
