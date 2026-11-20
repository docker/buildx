package kubernetes

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/driver/bkimage"
	"github.com/docker/buildx/driver/kubernetes/manifest"
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
			require.Equal(t, int32(2), r.Replicas)
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
			require.Equal(t, int32(1), r.Replicas)
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
			require.Equal(t, int32(1), r.Replicas)
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

func TestRequiresUncachedClient(t *testing.T) {
	f := factory{
		cc: &mockClientConfig{
			clientConfig: &rest.Config{},
		},
	}
	baseCfg := driver.InitConfig{
		Name: driver.BuilderName("test"),
	}

	t.Run("RandomLoadbalance", func(t *testing.T) {
		cfg := baseCfg
		cfg.DriverOpts = map[string]string{"loadbalance": "random"}
		d, err := f.New(t.Context(), cfg)
		require.NoError(t, err)
		require.True(t, d.(*Driver).RequiresUncachedClient(),
			"expected RequiresUncachedClient=true for loadbalance=random")
	})

	t.Run("StickyLoadbalance", func(t *testing.T) {
		cfg := baseCfg
		cfg.DriverOpts = map[string]string{"loadbalance": "sticky"}
		d, err := f.New(t.Context(), cfg)
		require.NoError(t, err)
		require.False(t, d.(*Driver).RequiresUncachedClient(),
			"expected RequiresUncachedClient=false for loadbalance=sticky")
	})

	t.Run("DefaultLoadbalance", func(t *testing.T) {
		cfg := baseCfg
		cfg.DriverOpts = map[string]string{}
		d, err := f.New(t.Context(), cfg)
		require.NoError(t, err)
		require.False(t, d.(*Driver).RequiresUncachedClient(),
			"expected RequiresUncachedClient=false for default (sticky) loadbalance")
	})
}

func TestTopologySpread(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		for _, name := range []string{"builder-a", "builder-b"} {
			t.Run(fmt.Sprintf("%s/persistent=%t", name, persistent), func(t *testing.T) {
				opts := map[string]string{
					"topology-spread.topologyKey":   "kubernetes.io/hostname",
					"topology-spread.labelSelector": "build-pool=shared",
					"labels":                        "build-pool=shared",
				}
				if persistent {
					opts["persistent-volume-claim.requests.storage"] = "1Gi"
				}
				opt, _, _, _, _, err := (&factory{}).processDriverOpts(name, "default", driver.InitConfig{DriverOpts: opts})
				require.NoError(t, err)
				d, s, _, err := manifest.NewDeployment(opt)
				require.NoError(t, err)
				var pod v1.PodTemplateSpec
				if persistent {
					pod = s.Spec.Template
				} else {
					pod = d.Spec.Template
				}
				require.Equal(t, name, pod.Labels["app"])
				require.Equal(t, "shared", pod.Labels["build-pool"])
				require.Len(t, pod.Spec.TopologySpreadConstraints, 1)
				spread := pod.Spec.TopologySpreadConstraints[0]
				require.Equal(t, int32(1), spread.MaxSkew)
				require.Equal(t, v1.ScheduleAnyway, spread.WhenUnsatisfiable)
				require.Equal(t, map[string]string{"build-pool": "shared"}, spread.LabelSelector.MatchLabels)
			})
		}
	}
	for _, tc := range []struct {
		name      string
		opts      map[string]string
		wantError string
	}{
		{
			name:      "missing key",
			opts:      map[string]string{"topology-spread.maxSkew": "1"},
			wantError: "topologyKey is required",
		},
		{
			name:      "invalid key",
			opts:      map[string]string{"topology-spread.topologyKey": "bad key"},
			wantError: "invalid topology-spread.topologyKey",
		},
		{
			name:      "zero skew",
			opts:      map[string]string{"topology-spread.maxSkew": "0"},
			wantError: "positive 32-bit integer",
		},
		{
			name:      "overflow",
			opts:      map[string]string{"topology-spread.maxSkew": "2147483648"},
			wantError: "positive 32-bit integer",
		},
		{
			name:      "invalid action",
			opts:      map[string]string{"topology-spread.whenUnsatisfiable": "never"},
			wantError: "expected ScheduleAnyway or DoNotSchedule",
		},
		{
			name:      "empty selector",
			opts:      map[string]string{"topology-spread.labelSelector": ""},
			wantError: "must not be empty",
		},
		{
			name:      "malformed selector",
			opts:      map[string]string{"topology-spread.labelSelector": "a in ("},
			wantError: "invalid topology-spread.labelSelector",
		},
		{
			name:      "nonmatching selector",
			opts:      map[string]string{"topology-spread.topologyKey": "kubernetes.io/hostname", "topology-spread.labelSelector": "pool=other"},
			wantError: "must match",
		},
		{
			name:      "unknown option",
			opts:      map[string]string{"topology-spread.unknown": "x"},
			wantError: "invalid driver option",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, _, err := (&factory{}).processDriverOpts("builder", "default", driver.InitConfig{DriverOpts: tc.opts})
			require.ErrorContains(t, err, tc.wantError)
		})
	}
	t.Run("default selector and hard constraint", func(t *testing.T) {
		opt, _, _, _, _, err := (&factory{}).processDriverOpts("builder", "default", driver.InitConfig{DriverOpts: map[string]string{
			"topology-spread.topologyKey":       "kubernetes.io/hostname",
			"topology-spread.maxSkew":           "2",
			"topology-spread.whenUnsatisfiable": "DoNotSchedule",
		}})
		require.NoError(t, err)
		spread := opt.TopologySpreadConstraints[0]
		require.Equal(t, int32(2), spread.MaxSkew)
		require.Equal(t, v1.DoNotSchedule, spread.WhenUnsatisfiable)
		require.Equal(t, map[string]string{"app": "builder"}, spread.LabelSelector.MatchLabels)
	})
	t.Run("set selector", func(t *testing.T) {
		opt, _, _, _, _, err := (&factory{}).processDriverOpts("builder", "default", driver.InitConfig{DriverOpts: map[string]string{
			"topology-spread.topologyKey":   "kubernetes.io/hostname",
			"topology-spread.labelSelector": "pool in (a,b)",
			"labels":                        "pool=a",
		}})
		require.NoError(t, err)
		expressions := opt.TopologySpreadConstraints[0].LabelSelector.MatchExpressions
		require.Len(t, expressions, 1)
		require.Equal(t, "pool", expressions[0].Key)
		require.EqualValues(t, "In", expressions[0].Operator)
		require.Equal(t, []string{"a", "b"}, expressions[0].Values)
	})
	t.Run("disabled", func(t *testing.T) {
		opt, _, _, _, _, err := (&factory{}).processDriverOpts("builder", "default", driver.InitConfig{})
		require.NoError(t, err)
		require.Nil(t, opt.TopologySpreadConstraints)
	})
}
