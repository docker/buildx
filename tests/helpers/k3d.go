package helpers

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

const (
	k3dBin    = "k3d"
	dockerBin = "docker"

	k3dCreateTimeout     = 3 * time.Minute
	k3dKubeconfigTimeout = 30 * time.Second
	k3dDeleteTimeout     = 30 * time.Second
	k3dInspectTimeout    = 30 * time.Second
	K3dRegistryPortCount = 16
)

func NewK3dServer(ctx context.Context, cfg *integration.BackendConfig, dockerAddress string) (clusterName, kubeConfig string, cl func() error, err error) {
	if _, err := exec.LookPath(k3dBin); err != nil {
		return "", "", nil, errors.Wrapf(err, "failed to lookup %s binary", k3dBin)
	}

	deferF := &integration.MultiCloser{}
	cl = deferF.F()

	defer func() {
		if err != nil {
			deferF.F()()
			cl = nil
		}
	}()

	clusterName = "bk-" + identity.NewID()

	createCtx, cancelCreate := context.WithTimeoutCause(ctx, k3dCreateTimeout, errors.New("timed out creating k3d cluster"))
	defer cancelCreate()

	args := []string{
		"cluster", "create", clusterName,
		"--wait",
		"--k3s-arg=--debug@server:0",
		"--k3s-arg=--snapshotter=native@server:0",
	}
	if image := KubernetesK3sImage(); image != "" {
		args = append(args, "--image="+image)
	}
	cmd := exec.CommandContext(createCtx, k3dBin, args...)
	cmd.Env = k3dEnv(dockerAddress)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cause := context.Cause(createCtx); cause != nil && cause != context.Canceled {
			err = cause
		}
		diag := KubernetesDiagnostics(ctx, clusterName, dockerAddress)
		return "", "", nil, errors.Wrapf(err, "failed to create k3d cluster %s: %s\n%s\nouter dockerd logs: %s", clusterName, strings.TrimSpace(string(out)), diag, integration.FormatLogs(cfg.Logs))
	}
	deferF.Append(func() error {
		deleteCtx, cancelDelete := context.WithTimeoutCause(context.WithoutCancel(ctx), k3dDeleteTimeout, errors.New("timed out deleting k3d cluster"))
		defer cancelDelete()
		cmd := exec.CommandContext(deleteCtx, k3dBin, "cluster", "delete", clusterName)
		cmd.Env = k3dEnv(dockerAddress)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errors.Wrapf(err, "failed to delete k3d cluster %s: %s", clusterName, string(out))
		}
		return nil
	})

	kubeconfigCtx, cancelKubeconfig := context.WithTimeoutCause(ctx, k3dKubeconfigTimeout, errors.New("timed out writing k3d kubeconfig"))
	defer cancelKubeconfig()

	cmd = exec.CommandContext(kubeconfigCtx, k3dBin, "kubeconfig", "write", clusterName)
	cmd.Env = k3dEnv(dockerAddress)
	out, err = cmd.CombinedOutput()
	if err != nil {
		if cause := context.Cause(kubeconfigCtx); cause != nil && cause != context.Canceled {
			err = cause
		}
		diag := KubernetesDiagnostics(ctx, clusterName, dockerAddress)
		return "", "", nil, errors.Wrapf(err, "failed to write kubeconfig for cluster %s: %s\n%s\nouter dockerd logs: %s", clusterName, strings.TrimSpace(string(out)), diag, integration.FormatLogs(cfg.Logs))
	}
	kubeConfig = strings.TrimSpace(string(out))

	return
}

func k3dEnv(dockerAddress string) []string {
	env := append(
		os.Environ(),
		"DOCKER_CONTEXT="+dockerAddress,
	)
	if image := KubernetesK3DToolsImage(); image != "" {
		env = append(env, "K3D_IMAGE_TOOLS="+image)
	}
	if image := KubernetesK3DLoadBalancerImage(); image != "" {
		env = append(env, "K3D_IMAGE_LOADBALANCER="+image)
	}
	return env
}

func K3dNetworkGateway(ctx context.Context, clusterName, dockerAddress string) (string, error) {
	inspectCtx, cancelInspect := context.WithTimeoutCause(ctx, k3dInspectTimeout, errors.New("timed out inspecting k3d network"))
	defer cancelInspect()

	cmd := exec.CommandContext(inspectCtx, dockerBin, "network", "inspect", "k3d-"+clusterName, "--format", "{{(index .IPAM.Config 0).Gateway}}")
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerAddress)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cause := context.Cause(inspectCtx); cause != nil && cause != context.Canceled {
			err = cause
		}
		return "", errors.Wrapf(err, "failed to inspect k3d network %s: %s", clusterName, strings.TrimSpace(string(out)))
	}
	gateway := strings.TrimSpace(string(out))
	if gateway == "" {
		return "", errors.Errorf("empty gateway for k3d network %s", clusterName)
	}
	return gateway, nil
}

func ReserveK3dRegistryPorts(count int) ([]int, error) {
	listeners := make([]net.Listener, 0, count)
	ports := make([]int, 0, count)
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()

	for i := 0; i < count; i++ {
		l, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, l)
		addr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			return nil, errors.Errorf("unexpected registry listener address %T", l.Addr())
		}
		ports = append(ports, addr.Port)
	}

	return ports, nil
}

func K3dRegistryConfig(host string, ports []int) integration.ConfigUpdater {
	return k3dRegistryConfig{
		host:  host,
		ports: append([]int(nil), ports...),
	}
}

type k3dRegistryConfig struct {
	host  string
	ports []int
}

func (rc k3dRegistryConfig) UpdateConfigFile(in string) (string, func() error) {
	if rc.host == "" || len(rc.ports) == 0 {
		return in, nil
	}

	var b strings.Builder
	b.WriteString(in)
	for _, port := range rc.ports {
		fmt.Fprintf(&b, `

[registry.%q]
  http = true
  insecure = true
`, net.JoinHostPort(rc.host, strconv.Itoa(port)))
	}
	return b.String(), nil
}
