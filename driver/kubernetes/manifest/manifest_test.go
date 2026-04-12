package manifest

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestNewDeploymentManifestPatch(t *testing.T) {
	controller := true
	blockOwnerDeletion := false

	d, s, c, err := NewDeployment(&DeploymentOpt{
		Namespace:     "test-ns",
		Name:          "test-builder",
		Image:         "buildkit:test",
		Replicas:      1,
		ManifestPatch: `.metadata.ownerReferences = [{apiVersion: "actions.github.com/v1alpha1", kind: "EphemeralRunner", name: "runner-xyz", uid: "b636330d-26b7-417a-8464-c2641438feed", controller: true, blockOwnerDeletion: false}]`,
	})
	require.NoError(t, err)
	require.NotNil(t, d)
	require.Nil(t, s)
	require.Empty(t, c)
	require.Equal(t, []metav1.OwnerReference{
		{
			APIVersion:         "actions.github.com/v1alpha1",
			Kind:               "EphemeralRunner",
			Name:               "runner-xyz",
			UID:                types.UID("b636330d-26b7-417a-8464-c2641438feed"),
			Controller:         &controller,
			BlockOwnerDeletion: &blockOwnerDeletion,
		},
	}, d.OwnerReferences)
}

func TestNewDeploymentManifestPatchInvalid(t *testing.T) {
	_, _, _, err := NewDeployment(&DeploymentOpt{
		Namespace:     "test-ns",
		Name:          "test-builder",
		Image:         "buildkit:test",
		Replicas:      1,
		ManifestPatch: `.metadata.ownerReferences = [`,
	})
	require.Error(t, err)
}
