package helpers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultTestBuildkitTag = "buildx-stable-1"

func KubernetesBuildkitImage() string {
	if v := os.Getenv("TEST_BUILDKIT_IMAGE"); v != "" {
		return v
	}
	tag := os.Getenv("TEST_BUILDKIT_TAG")
	if tag == "" {
		tag = defaultTestBuildkitTag
	}
	return "moby/buildkit:" + tag
}

func KubernetesK3sImage() string {
	return os.Getenv("TEST_K3S_IMAGE")
}

func KubernetesK3DToolsImage() string {
	return os.Getenv("TEST_K3D_TOOLS_IMAGE")
}

func KubernetesK3DLoadBalancerImage() string {
	return os.Getenv("TEST_K3D_LOADBALANCER_IMAGE")
}

func KubernetesDiagnostics(clusterName, dockerContext string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var buf bytes.Buffer
	appendK3dDiagnostics(ctx, &buf, clusterName, dockerContext)
	appendDockerDiagnostics(ctx, &buf, dockerContext)
	appendK3sServerDiagnostics(ctx, &buf, clusterName, dockerContext)
	return strings.TrimSpace(buf.String())
}

func appendK3dDiagnostics(ctx context.Context, buf *bytes.Buffer, clusterName, dockerContext string) {
	appendCommandOutput(ctx, buf, "k3d cluster list", "k3d", []string{"cluster", "list", clusterName}, []string{"DOCKER_CONTEXT=" + dockerContext})
	appendCommandOutput(ctx, buf, "k3d node list", "k3d", []string{"node", "list"}, []string{"DOCKER_CONTEXT=" + dockerContext})
}

func appendDockerDiagnostics(ctx context.Context, buf *bytes.Buffer, dockerContext string) {
	args := []string{"ps", "-a", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}"}
	appendCommandOutput(ctx, buf, "docker ps", "docker", args, []string{"DOCKER_CONTEXT=" + dockerContext})
}

func appendK3sServerDiagnostics(ctx context.Context, buf *bytes.Buffer, clusterName, dockerContext string) {
	nodeNames, err := clusterNodeNames(ctx, clusterName, dockerContext)
	if err != nil {
		fmt.Fprintf(buf, "cluster node discovery error: %v\n", err)
		return
	}
	if len(nodeNames) == 0 {
		fmt.Fprintln(buf, "cluster node discovery: no matching k3d containers found")
		return
	}

	for _, nodeName := range nodeNames {
		appendCommandOutput(ctx, buf, "docker inspect "+nodeName, "docker", []string{
			"inspect",
			"--format",
			"Status={{.State.Status}} Health={{if .State.Health}}{{.State.Health.Status}}{{else}}<none>{{end}} Restarting={{.State.Restarting}} ExitCode={{.State.ExitCode}} Error={{.State.Error}} Privileged={{.HostConfig.Privileged}} Cgroupns={{.HostConfig.CgroupnsMode}}",
			nodeName,
		}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker logs "+nodeName, "docker", []string{"logs", "--tail", "80", nodeName}, []string{"DOCKER_CONTEXT=" + dockerContext})
	}

	for _, nodeName := range nodeNames {
		if !strings.Contains(nodeName, "-server-") {
			continue
		}
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" ps", "docker", []string{"exec", nodeName, "sh", "-c", "ps auxww"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" sockets", "docker", []string{"exec", nodeName, "sh", "-c", "ss -lntp || netstat -lnt"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" cgroup", "docker", []string{"exec", nodeName, "sh", "-c", "cat /proc/1/cgroup && echo && mount | grep cgroup"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" env", "docker", []string{"exec", nodeName, "sh", "-c", "env | sort"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" entrypoint", "docker", []string{"exec", nodeName, "sh", "-c", "sed -n '1,200p' /bin/k3d-entrypoint.sh"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" k3s files", "docker", []string{"exec", nodeName, "sh", "-c", "find /var/lib/rancher/k3s -maxdepth 3 -type f 2>/dev/null | sort"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" k3s logs", "docker", []string{"exec", nodeName, "sh", "-c", "for f in /var/log/k3s.log /var/lib/rancher/k3s/agent/containerd/containerd.log /var/lib/rancher/k3s/server/logs/*; do if [ -f \"$f\" ]; then echo \"== $f ==\"; tail -n 200 \"$f\"; echo; fi; done"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" kubectl get pods", "docker", []string{"exec", nodeName, "kubectl", "get", "pods", "-A", "-o", "wide"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" kubectl get events", "docker", []string{"exec", nodeName, "kubectl", "get", "events", "-A", "--sort-by=.lastTimestamp"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		appendCommandOutput(ctx, buf, "docker exec "+nodeName+" kubectl describe pods", "docker", []string{"exec", nodeName, "kubectl", "describe", "pods", "-A"}, []string{"DOCKER_CONTEXT=" + dockerContext})
		break
	}
}

func clusterNodeNames(ctx context.Context, clusterName, dockerContext string) ([]string, error) {
	out, err := runCommand(ctx, "docker", []string{
		"ps", "-a",
		"--filter", "name=k3d-" + clusterName,
		"--format", "{{.Names}}",
	}, []string{"DOCKER_CONTEXT=" + dockerContext})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	return names, nil
}

func appendCommandOutput(ctx context.Context, buf *bytes.Buffer, title, name string, args []string, env []string) {
	out, err := runCommand(ctx, name, args, env)
	fmt.Fprintf(buf, "== %s ==\n", title)
	if err != nil {
		fmt.Fprintf(buf, "error: %v\n", err)
	}
	if strings.TrimSpace(out) == "" {
		fmt.Fprintln(buf, "<empty>")
	} else {
		fmt.Fprintf(buf, "%s\n", strings.TrimSpace(out))
	}
	fmt.Fprintln(buf)
}

func runCommand(ctx context.Context, name string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}
