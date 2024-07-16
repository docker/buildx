package kubernetes

import (
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/driver/bkimage"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

type mockClientConfig struct {
	clientConfig *rest.Config
	namespace    string
}

func (r *mockClientConfig) ClientConfig() (*rest.Config, error) {
	return r.clientConfig, nil
}

func (r *mockClientConfig) Namespace() (string, bool, error) {
	return r.namespace, true, nil
}

func TestFactory_processDriverOpts(t *testing.T) {
	cfg := driver.InitConfig{
		Name: driver.BuilderName("test"),
	}
	f := factory{
		cc: &mockClientConfig{
			clientConfig: &rest.Config{},
		},
	}

	t.Run(
		"ValidOptions", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"namespace":       "test-ns",
				"image":           "test:latest",
				"replicas":        "2",
				"timeout":         "300s",
				"requests.cpu":    "100m",
				"requests.memory": "32Mi",
				"limits.cpu":      "200m",
				"limits.memory":   "64Mi",
				"rootless":        "true",
				"nodeselector":    "selector1=value1,selector2=value2",
				"tolerations":     "key=tolerationKey1,value=tolerationValue1,operator=Equal,effect=NoSchedule,tolerationSeconds=60;key=tolerationKey2,operator=Exists",
				"annotations":     "example.com/expires-after=annotation1,example.com/other=annotation2",
				"labels":          "example.com/owner=label1,example.com/other=label2",
				"loadbalance":     "random",
				"qemu.install":    "true",
				"qemu.image":      "qemu:latest",
				"default-load":    "true",
			}
			r, loadbalance, ns, defaultLoad, timeout, err := f.processDriverOpts(cfg.Name, "test", cfg)

			nodeSelectors := map[string]string{
				"selector1": "value1",
				"selector2": "value2",
			}

			ts := int64(60)
			tolerations := []v1.Toleration{
				{
					Key:               "tolerationKey1",
					Operator:          v1.TolerationOpEqual,
					Value:             "tolerationValue1",
					Effect:            v1.TaintEffectNoSchedule,
					TolerationSeconds: &ts,
				},
				{
					Key:      "tolerationKey2",
					Operator: v1.TolerationOpExists,
				},
			}

			customAnnotations := map[string]string{
				"example.com/expires-after": "annotation1",
				"example.com/other":         "annotation2",
			}

			customLabels := map[string]string{
				"example.com/owner": "label1",
				"example.com/other": "label2",
			}

			require.NoError(t, err)

			require.Equal(t, "test-ns", ns)
			require.Equal(t, "test:latest", r.Image)
			require.Equal(t, 2, r.Replicas)
			require.Equal(t, "100m", r.RequestsCPU)
			require.Equal(t, "32Mi", r.RequestsMemory)
			require.Equal(t, "200m", r.LimitsCPU)
			require.Equal(t, "64Mi", r.LimitsMemory)
			require.True(t, r.Rootless)
			require.Equal(t, nodeSelectors, r.NodeSelector)
			require.Equal(t, customAnnotations, r.CustomAnnotations)
			require.Equal(t, customLabels, r.CustomLabels)
			require.Equal(t, tolerations, r.Tolerations)
			require.Equal(t, LoadbalanceRandom, loadbalance)
			require.True(t, r.Qemu.Install)
			require.Equal(t, "qemu:latest", r.Qemu.Image)
			require.True(t, defaultLoad)
			require.Equal(t, 300*time.Second, timeout)
		},
	)

	t.Run(
		"NoOptions", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{}

			r, loadbalance, ns, defaultLoad, timeout, err := f.processDriverOpts(cfg.Name, "test", cfg)

			require.NoError(t, err)

			require.Equal(t, "test", ns)
			require.Equal(t, bkimage.DefaultImage, r.Image)
			require.Equal(t, 1, r.Replicas)
			require.Equal(t, "", r.RequestsCPU)
			require.Equal(t, "", r.RequestsMemory)
			require.Equal(t, "", r.LimitsCPU)
			require.Equal(t, "", r.LimitsMemory)
			require.False(t, r.Rootless)
			require.Empty(t, r.NodeSelector)
			require.Empty(t, r.CustomAnnotations)
			require.Empty(t, r.CustomLabels)
			require.Empty(t, r.Tolerations)
			require.Equal(t, LoadbalanceSticky, loadbalance)
			require.False(t, r.Qemu.Install)
			require.Equal(t, bkimage.QemuImage, r.Qemu.Image)
			require.False(t, defaultLoad)
			require.Equal(t, 120*time.Second, timeout)
		},
	)

	t.Run(
		"RootlessOverride", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"rootless":    "true",
				"loadbalance": "sticky",
			}

			r, loadbalance, ns, defaultLoad, timeout, err := f.processDriverOpts(cfg.Name, "test", cfg)

			require.NoError(t, err)

			require.Equal(t, "test", ns)
			require.Equal(t, bkimage.DefaultRootlessImage, r.Image)
			require.Equal(t, 1, r.Replicas)
			require.Equal(t, "", r.RequestsCPU)
			require.Equal(t, "", r.RequestsMemory)
			require.Equal(t, "", r.LimitsCPU)
			require.Equal(t, "", r.LimitsMemory)
			require.True(t, r.Rootless)
			require.Empty(t, r.NodeSelector)
			require.Empty(t, r.CustomAnnotations)
			require.Empty(t, r.CustomLabels)
			require.Empty(t, r.Tolerations)
			require.Equal(t, LoadbalanceSticky, loadbalance)
			require.False(t, r.Qemu.Install)
			require.Equal(t, bkimage.QemuImage, r.Qemu.Image)
			require.False(t, defaultLoad)
			require.Equal(t, 120*time.Second, timeout)
		},
	)

	t.Run(
		"InvalidReplicas", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"replicas": "invalid",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidRootless", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"rootless": "invalid",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidTolerationKeyword", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"tolerations": "key=foo,value=bar,invalid=foo2",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidTolerationSeconds", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"tolerations": "key=foo,value=bar,tolerationSeconds=invalid",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidCustomAnnotation", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"annotations": "key,value",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidCustomLabel", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"labels": "key=value=foo",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidLoadBalance", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"loadbalance": "invalid",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidQemuInstall", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"qemu.install": "invalid",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidOption", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"invalid": "foo",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)

	t.Run(
		"InvalidTimeout", func(t *testing.T) {
			cfg.DriverOpts = map[string]string{
				"timeout": "invalid",
			}
			_, _, _, _, _, err := f.processDriverOpts(cfg.Name, "test", cfg)
			require.Error(t, err)
		},
	)
}
