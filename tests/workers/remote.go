package workers

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

var protos = []string{
	"docker-container",
	"tcp",
}

func InitRemoteWorker() {
	for _, p := range protos {
		integration.Register(&remoteWorker{
			id:    "remote+" + p,
			proto: p,
		})
	}
}

type remoteWorker struct {
	id    string
	proto string

	unsupported []string

	docker      integration.Backend
	dockerClose func() error
	dockerErr   error
	dockerOnce  sync.Once
}

func (w *remoteWorker) Name() string {
	return w.id
}

func (w *remoteWorker) Rootless() bool {
	return false
}

func (w *remoteWorker) NetNSDetached() bool {
	return false
}

func (w *remoteWorker) New(ctx context.Context, cfg *integration.BackendConfig) (b integration.Backend, cl func() error, err error) {
	w.dockerOnce.Do(func() {
		w.docker, w.dockerClose, w.dockerErr = dockerWorker{id: w.id}.New(ctx, cfg)
	})
	if w.dockerErr != nil {
		return w.docker, w.dockerClose, w.dockerErr
	}

	bkCtnName := "buildkit-integration-" + identity.NewID()
	name := "integration-remote-" + identity.NewID()
	envs := append(
		os.Environ(),
		"BUILDX_CONFIG=/tmp/buildx-"+name,
		"DOCKER_CONTEXT="+w.docker.DockerAddress(),
	)

	// random host port for buildkit container
	l, _ := net.Listen("tcp", ":0") //nolint:gosec
	_ = l.Close()
	bkPort := l.Addr().(*net.TCPAddr).Port

	// create buildkit container
	bkCtnCmd := exec.Command("docker", "run",
		"-d", "--rm",
		"--privileged",
		"-p", fmt.Sprintf("%d:1234", bkPort),
		"--name="+bkCtnName,
		"moby/buildkit:buildx-stable-1",
		"--addr=tcp://0.0.0.0:1234",
	)
	bkCtnCmd.Env = envs
	if out, err := bkCtnCmd.CombinedOutput(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildkit container %s: %s", bkCtnName, string(out))
	}

	// create builder
	var endpoint string
	switch w.proto {
	case "docker-container":
		endpoint = fmt.Sprintf("docker-container://%s", bkCtnName)
	case "tcp":
		endpoint = fmt.Sprintf("tcp://localhost:%d", bkPort)
	default:
		return nil, nil, errors.Errorf("unsupported protocol %s", w.proto)
	}
	cmd := exec.Command("buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--driver=remote",
		endpoint,
	)
	cmd.Env = envs
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s: %s", name, string(out))
	}

	cl = func() error {
		cmd := exec.Command("docker", "container", "rm", "-f", name)
		cmd.Env = envs
		if err1 := cmd.Run(); err1 != nil {
			err = errors.Wrapf(err1, "failed to remove buildkit container %s", bkCtnName)
		}
		cmd = exec.Command("buildx", "rm", "-f", name)
		cmd.Env = envs
		if err1 := cmd.Run(); err1 != nil {
			err = errors.Wrapf(err1, "failed to remove buildx instance %s", name)
		}
		return err
	}

	return &backend{
		builder:             name,
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func (w *remoteWorker) Close() error {
	return nil
}
