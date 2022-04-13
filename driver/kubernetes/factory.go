package kubernetes

import (
	"context"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/driver/bkimage"
	"github.com/docker/buildx/driver/kubernetes/manifest"
	"github.com/docker/buildx/driver/kubernetes/podchooser"
	dockerclient "github.com/docker/docker/client"
	"github.com/pkg/errors"
	"k8s.io/client-go/kubernetes"
)

const prioritySupported = 40
const priorityUnsupported = 80

func init() {
	driver.Register(&factory{})
}

type factory struct {
}

func (*factory) Name() string {
	return DriverName
}

func (*factory) Usage() string {
	return DriverName
}

func (*factory) Priority(ctx context.Context, api dockerclient.APIClient) int {
	if api == nil {
		return priorityUnsupported
	}
	return prioritySupported
}

func (f *factory) New(ctx context.Context, cfg driver.InitConfig) (driver.Driver, error) {
	if cfg.KubeClientConfig == nil {
		return nil, errors.Errorf("%s driver requires kubernetes API access", DriverName)
	}
	deploymentName, err := buildxNameToDeploymentName(cfg.Name)
	if err != nil {
		return nil, err
	}
	namespace, _, err := cfg.KubeClientConfig.Namespace()
	if err != nil {
		return nil, errors.Wrap(err, "cannot determine Kubernetes namespace, specify manually")
	}
	restClientConfig, err := cfg.KubeClientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(restClientConfig)
	if err != nil {
		return nil, err
	}

	d := &Driver{
		factory:    f,
		InitConfig: cfg,
		clientset:  clientset,
	}

	deploymentOpt := &manifest.DeploymentOpt{
		Name:          deploymentName,
		Image:         bkimage.DefaultImage,
		Replicas:      1,
		BuildkitFlags: cfg.BuildkitFlags,
		Rootless:      false,
		Platforms:     cfg.Platforms,
		ConfigFiles:   cfg.Files,
	}

	deploymentOpt.Qemu.Image = bkimage.QemuImage

	loadbalance := LoadbalanceSticky

	for k, v := range cfg.DriverOpts {
		switch k {
		case "image":
			if v != "" {
				deploymentOpt.Image = v
			}
		case "namespace":
			namespace = v
		case "replicas":
			deploymentOpt.Replicas, err = strconv.Atoi(v)
			if err != nil {
				return nil, err
			}
		case "requests.cpu":
			deploymentOpt.RequestsCPU = v
		case "requests.memory":
			deploymentOpt.RequestsMemory = v
		case "limits.cpu":
			deploymentOpt.LimitsCPU = v
		case "limits.memory":
			deploymentOpt.LimitsMemory = v
		case "rootless":
			deploymentOpt.Rootless, err = strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
			if _, isImage := cfg.DriverOpts["image"]; !isImage {
				deploymentOpt.Image = bkimage.DefaultRootlessImage
			}
		case "nodeselector":
			kvs := strings.Split(strings.Trim(v, `"`), ",")
			s := map[string]string{}
			for i := range kvs {
				kv := strings.Split(kvs[i], "=")
				if len(kv) == 2 {
					s[kv[0]] = kv[1]
				}
			}
			deploymentOpt.NodeSelector = s
		case "tolerations":
			u, err := strconv.Unquote(v)
			if nil != err {
				return nil, err
			}
			ts := strings.Split(u, ";")
			deploymentOpt.Tolerations = []corev1.Toleration{}
			for i := range ts {
				kvs := strings.Split(ts[i], ",")
				if len(kvs) == 0 {
					return nil, errors.Errorf("invalid tolaration %q", v)
				}

				t := corev1.Toleration{}

				for j := range kvs {
					kv := strings.Split(kvs[j], "=")
					if len(kv) == 2 {
						switch kv[0] {
						case "key":
							t.Key = kv[1]
						case "operator":
							t.Operator = corev1.TolerationOperator(kv[1])
						case "value":
							t.Value = kv[1]
						case "effect":
							t.Effect = corev1.TaintEffect(kv[1])
						case "tolerationSeconds":
							c, err := strconv.Atoi(kv[1])
							if nil != err {
								return nil, err
							}
							c64 := int64(c)
							t.TolerationSeconds = &c64
						default:
							return nil, errors.Errorf("invalid tolaration %q", v)
						}
					}
				}

				deploymentOpt.Tolerations = append(deploymentOpt.Tolerations, t)
			}
		case "loadbalance":
			switch v {
			case LoadbalanceSticky:
			case LoadbalanceRandom:
			default:
				return nil, errors.Errorf("invalid loadbalance %q", v)
			}
			loadbalance = v
		case "qemu.install":
			deploymentOpt.Qemu.Install, err = strconv.ParseBool(v)
			if err != nil {
				return nil, err
			}
		case "qemu.image":
			if v != "" {
				deploymentOpt.Qemu.Image = v
			}
		default:
			return nil, errors.Errorf("invalid driver option %s for driver %s", k, DriverName)
		}
	}

	d.deployment, d.configMaps, err = manifest.NewDeployment(deploymentOpt)
	if err != nil {
		return nil, err
	}

	d.minReplicas = deploymentOpt.Replicas

	d.deploymentClient = clientset.AppsV1().Deployments(namespace)
	d.podClient = clientset.CoreV1().Pods(namespace)
	d.configMapClient = clientset.CoreV1().ConfigMaps(namespace)

	switch loadbalance {
	case LoadbalanceSticky:
		d.podChooser = &podchooser.StickyPodChooser{
			Key:        cfg.ContextPathHash,
			PodClient:  d.podClient,
			Deployment: d.deployment,
		}
	case LoadbalanceRandom:
		d.podChooser = &podchooser.RandomPodChooser{
			PodClient:  d.podClient,
			Deployment: d.deployment,
		}
	}
	return d, nil
}

func (f *factory) AllowsInstances() bool {
	return true
}

// buildxNameToDeploymentName converts buildx name to Kubernetes Deployment name.
//
// eg. "buildx_buildkit_loving_mendeleev0" -> "loving-mendeleev0"
func buildxNameToDeploymentName(bx string) (string, error) {
	// TODO: commands.util.go should not pass "buildx_buildkit_" prefix to drivers
	if !strings.HasPrefix(bx, "buildx_buildkit_") {
		return "", errors.Errorf("expected a string with \"buildx_buildkit_\", got %q", bx)
	}
	s := strings.TrimPrefix(bx, "buildx_buildkit_")
	s = strings.ReplaceAll(s, "_", "-")
	return s, nil
}
