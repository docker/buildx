package workers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/docker/buildx/driver"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

func InitRemoteMultiNodeWorker() {
	integration.Register(&remoteMultiNodeWorker{
		id: "remote+multinode",
	})
}

type remoteMultiNodeWorker struct {
	id string

	unsupported []string

	docker      integration.Backend
	dockerClose func() error
	dockerErr   error
	dockerOnce  sync.Once
}

func (w *remoteMultiNodeWorker) Name() string {
	return w.id
}

func (w *remoteMultiNodeWorker) Rootless() bool {
	return false
}

func (w *remoteMultiNodeWorker) NetNSDetached() bool {
	return false
}

func (w *remoteMultiNodeWorker) New(ctx context.Context, cfg *integration.BackendConfig) (integration.Backend, func() error, error) {
	w.dockerOnce.Do(func() {
		w.docker, w.dockerClose, w.dockerErr = dockerWorker{id: w.id}.New(ctx, cfg)
	})
	if w.dockerErr != nil {
		return w.docker, w.dockerClose, w.dockerErr
	}

	cfgfile, release, err := integration.WriteConfig(cfg.DaemonConfig)
	if err != nil {
		return nil, nil, err
	}
	if release != nil {
		defer release()
	}
	defer os.RemoveAll(filepath.Dir(cfgfile))

	name := "integration-remote-multinode-" + identity.NewID()
	ctnBuilder0 := name + "-amd64"
	ctnBuilder1 := name + "-arm64"

	run := func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "buildx", args...)
		cmd.Env = append(
			os.Environ(),
			"BUILDX_CONFIG=/tmp/buildx-"+name,
			"DOCKER_CONTEXT="+w.docker.DockerAddress(),
		)
		return cmd.CombinedOutput()
	}

	if out, err := run(ctx, "create",
		"--name="+ctnBuilder0,
		"--driver=docker-container",
		"--buildkitd-config="+cfgfile,
		"--driver-opt=network=host",
		"--platform=linux/amd64",
	); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create builder %s: %s", ctnBuilder0, string(out))
	}
	if out, err := run(ctx, "inspect", "--bootstrap", ctnBuilder0); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bootstrap builder %s: %s", ctnBuilder0, string(out))
	}

	if out, err := run(ctx, "create",
		"--name="+ctnBuilder1,
		"--driver=docker-container",
		"--buildkitd-config="+cfgfile,
		"--driver-opt=network=host",
		"--platform=linux/arm64",
	); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create builder %s: %s", ctnBuilder1, string(out))
	}
	if out, err := run(ctx, "inspect", "--bootstrap", ctnBuilder1); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bootstrap builder %s: %s", ctnBuilder1, string(out))
	}

	endpoint0 := fmt.Sprintf("docker-container://%s0", driver.BuilderName(ctnBuilder0))
	endpoint1 := fmt.Sprintf("docker-container://%s0", driver.BuilderName(ctnBuilder1))
	if out, err := run(ctx, "create", "--name="+name, "--driver=remote", endpoint0); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create builder %s: %s", name, string(out))
	}
	if out, err := run(ctx, "create", "--append", "--name="+name, endpoint1); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to append builder %s: %s", name, string(out))
	}
	if out, err := run(ctx, "inspect", "--bootstrap", name); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bootstrap builder %s: %s", name, string(out))
	}

	cl := func() error {
		runCleanup := func(args ...string) error {
			out, err := run(context.Background(), args...)
			if err != nil {
				return errors.Wrapf(err, "%s: %s", args[1], string(out))
			}
			return nil
		}

		setErr := func(dst *error, err error) {
			if err != nil && *dst == nil {
				*dst = err
			}
		}

		var err error
		setErr(&err, runCleanup("rm", "-f", name))
		setErr(&err, runCleanup("rm", "-f", ctnBuilder0))
		setErr(&err, runCleanup("rm", "-f", ctnBuilder1))
		return err
	}

	return &backend{
		builder:             name,
		context:             w.docker.DockerAddress(),
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func (w *remoteMultiNodeWorker) Close() error {
	if c := w.dockerClose; c != nil {
		return c()
	}

	w.docker = nil
	w.dockerClose = nil
	w.dockerErr = nil
	w.dockerOnce = sync.Once{}

	return nil
}
