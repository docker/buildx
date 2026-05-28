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

	d, _, _, err := NewDeployment(opt)
	require.NoError(t, err)
	require.NotNil(t, d)

	podSpec := d.Spec.Template.Spec
	container := podSpec.Containers[0]

	vm := findVolumeMount(container.VolumeMounts, rootVolumePath)
	require.NotNil(t, vm, "expected volume mount at %s", rootVolumePath)

	vol := findVolume(podSpec.Volumes, rootVolumeName)
	require.NotNil(t, vol)
	require.NotNil(t, vol.EmptyDir)
	require.Equal(t, corev1.StorageMediumMemory, vol.EmptyDir.Medium)
	require.NotNil(t, vol.EmptyDir.SizeLimit)
	require.Equal(t, "2Gi", vol.EmptyDir.SizeLimit.String())
}
