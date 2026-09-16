package manifest

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func newBaseOpt() *DeploymentOpt {
	return &DeploymentOpt{
		Namespace: "test-ns",
		Name:      "test",
		Image:     "moby/buildkit:latest",
		Replicas:  1,
	}
}

func findVolumeMount(mounts []corev1.VolumeMount, mountPath string) *corev1.VolumeMount {
	for i := range mounts {
		if mounts[i].MountPath == mountPath {
			return &mounts[i]
		}
	}
	return nil
}

func findVolume(volumes []corev1.Volume, name string) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == name {
			return &volumes[i]
		}
	}
	return nil
}

func TestRootlessSecurityContext(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		name := "deployment"
		if persistent {
			name = "statefulset"
		}
		t.Run(name, func(t *testing.T) {
			opt := newBaseOpt()
			opt.Rootless = true
			opt.CustomAnnotations = map[string]string{
				"example.com/custom": "value",
				"container.apparmor.security.beta.kubernetes.io/" + containerName: "runtime/default",
			}
			if persistent {
				opt.RequestsPersistentStorage = "1Gi"
			}

			d, s, _, err := NewDeployment(opt)
			require.NoError(t, err)
			var pod corev1.PodTemplateSpec
			if persistent {
				require.NotNil(t, s)
				pod = s.Spec.Template
			} else {
				require.NotNil(t, d)
				pod = d.Spec.Template
			}

			require.Equal(t, &corev1.SecurityContext{
				AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined},
				SeccompProfile:  &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined},
			}, pod.Spec.Containers[0].SecurityContext)
			require.Equal(t, "value", pod.Annotations["example.com/custom"])
			require.NotContains(t, pod.Annotations, "container.apparmor.security.beta.kubernetes.io/"+containerName)
		})
	}
}

func TestRootlessMemoryVolume(t *testing.T) {
	opt := newBaseOpt()
	opt.Rootless = true
	opt.BuildKitRootVolumeMemory = "1Gi"

	d, _, _, err := NewDeployment(opt)
	require.NoError(t, err)
	require.NotNil(t, d)

	podSpec := d.Spec.Template.Spec
	container := podSpec.Containers[0]

	const rootlessDataPath = "/home/user/.local/share/buildkit"
	vm := findVolumeMount(container.VolumeMounts, rootlessDataPath)
	require.NotNil(t, vm, "expected volume mount at %s", rootlessDataPath)

	vol := findVolume(podSpec.Volumes, vm.Name)
	require.NotNil(t, vol, "expected volume with name %q", vm.Name)
	require.NotNil(t, vol.EmptyDir, "expected EmptyDir volume source")
	require.Equal(t, corev1.StorageMediumMemory, vol.EmptyDir.Medium,
		"rootless EmptyDir should use memory medium when BuildKitRootVolumeMemory is set")
	require.NotNil(t, vol.EmptyDir.SizeLimit,
		"rootless EmptyDir should have a SizeLimit when BuildKitRootVolumeMemory is set")
	require.Equal(t, "1Gi", vol.EmptyDir.SizeLimit.String())

	// /var/lib/buildkit is unused in rootless mode; no mount or volume should appear.
	vm2 := findVolumeMount(container.VolumeMounts, rootVolumePath)
	require.Nil(t, vm2, "rootless mode should not mount %s", rootVolumePath)

	vol2 := findVolume(podSpec.Volumes, rootVolumeName)
	require.Nil(t, vol2, "rootless mode should not create a separate %q volume", rootVolumeName)
}

func TestRootlessNoMemoryVolume(t *testing.T) {
	opt := newBaseOpt()
	opt.Rootless = true

	d, _, _, err := NewDeployment(opt)
	require.NoError(t, err)
	require.NotNil(t, d)

	podSpec := d.Spec.Template.Spec
	container := podSpec.Containers[0]

	const rootlessDataPath = "/home/user/.local/share/buildkit"
	vm := findVolumeMount(container.VolumeMounts, rootlessDataPath)
	require.NotNil(t, vm, "expected volume mount at %s", rootlessDataPath)

	vol := findVolume(podSpec.Volumes, vm.Name)
	require.NotNil(t, vol)
	require.NotNil(t, vol.EmptyDir)
	require.Equal(t, corev1.StorageMediumDefault, vol.EmptyDir.Medium,
		"rootless EmptyDir should use default medium when BuildKitRootVolumeMemory is not set")
	require.Nil(t, vol.EmptyDir.SizeLimit)
}

func TestNonRootlessMemoryVolume(t *testing.T) {
	opt := newBaseOpt()
	opt.BuildKitRootVolumeMemory = "2Gi"
	opt.CustomAnnotations = map[string]string{
		"container.apparmor.security.beta.kubernetes.io/" + containerName: "runtime/default",
	}

	d, _, _, err := NewDeployment(opt)
	require.NoError(t, err)
	require.NotNil(t, d)

	podSpec := d.Spec.Template.Spec
	container := podSpec.Containers[0]
	require.NotNil(t, container.SecurityContext.Privileged)
	require.True(t, *container.SecurityContext.Privileged)
	require.Nil(t, container.SecurityContext.AppArmorProfile)
	require.Equal(t, opt.CustomAnnotations, d.Spec.Template.Annotations)

	vm := findVolumeMount(container.VolumeMounts, rootVolumePath)
	require.NotNil(t, vm, "expected volume mount at %s", rootVolumePath)

	vol := findVolume(podSpec.Volumes, rootVolumeName)
	require.NotNil(t, vol)
	require.NotNil(t, vol.EmptyDir)
	require.Equal(t, corev1.StorageMediumMemory, vol.EmptyDir.Medium)
	require.NotNil(t, vol.EmptyDir.SizeLimit)
	require.Equal(t, "2Gi", vol.EmptyDir.SizeLimit.String())
}
