package workers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/dockerd"
	"github.com/moby/buildkit/util/testutil/integration"
	bkworkers "github.com/moby/buildkit/util/testutil/workers"
	"github.com/pkg/errors"
)

func InitDockerWorker() {
	integration.Register(&dockerWorker{
		id:     "docker",
		binary: dockerd.DefaultDockerdBinary,
	})
	integration.Register(&dockerWorker{
		id:                    "docker+containerd",
		binary:                dockerd.DefaultDockerdBinary,
		containerdSnapshotter: true,
	})
	// e.g. `docker@26.0=/opt/docker-26.0,docker@25.0=/opt/docker-25.0`
	if s := os.Getenv("TEST_DOCKER_EXTRA"); s != "" {
		entries := strings.Split(s, ",")
		for _, entry := range entries {
			ver, bin, err := func(entry string) (string, string, error) {
				p1 := strings.Split(strings.TrimSpace(entry), "=")
				if len(p1) != 2 {
					return "", "", errors.Errorf("invalid entry: %q", entry)
				}
				name, bin := p1[0], p1[1]
				if _, err := os.Stat(bin); err != nil {
					return "", "", errors.Wrapf(err, "bin not found: %q", bin)
				}
				p2 := strings.Split(strings.TrimSpace(name), "@")
				if len(p2) != 2 {
					return "", "", errors.Errorf("invalid name: %q", name)
				}
				_, ver := p2[0], p2[1]
				if ver == "" {
					return "", "", errors.New("empty version")
				}
				return ver, bin, nil
			}(entry)
			if err != nil {
				panic(errors.Wrapf(err, "unexpected TEST_DOCKER_EXTRA: %q", s))
			}
			integration.Register(&dockerWorker{
				id:       fmt.Sprintf("docker@%s", ver),
				binary:   filepath.Join(bin, "dockerd"),
				extraEnv: []string{fmt.Sprintf("PATH=%s:%s", bin, os.Getenv("PATH"))},
			})
			integration.Register(&dockerWorker{
				id:                    fmt.Sprintf("docker+containerd@%s", ver),
				binary:                filepath.Join(bin, "dockerd"),
				containerdSnapshotter: true,
				extraEnv:              []string{fmt.Sprintf("PATH=%s:%s", bin, os.Getenv("PATH"))},
			})
		}
	}
}

type dockerWorker struct {
	id                    string
	binary                string
	containerdSnapshotter bool
	unsupported           []string
	extraEnv              []string
}

func (c dockerWorker) Name() string {
	return c.id
}

func (c dockerWorker) Rootless() bool {
	return false
}

func (c *dockerWorker) NetNSDetached() bool {
	return false
}

func (c dockerWorker) New(ctx context.Context, cfg *integration.BackendConfig) (b integration.Backend, cl func() error, err error) {
	moby := bkworkers.Moby{
		ID:                    c.id,
		Binary:                c.binary,
		ContainerdSnapshotter: c.containerdSnapshotter,
		ExtraEnv:              c.extraEnv,
	}
	bk, bkclose, err := moby.New(ctx, cfg)
	if err != nil {
		return bk, cl, err
	}

	name := "integration-" + identity.NewID()
	cmd := exec.Command("docker", "context", "create",
		name,
		"--docker", "host="+bk.DockerAddress(),
	)
	cmd.Env = append(os.Environ(), "BUILDX_CONFIG=/tmp/buildx-"+name)
	if err := cmd.Run(); err != nil {
		return bk, cl, errors.Wrapf(err, "failed to create buildx instance %s", name)
	}

	cl = func() error {
		err := bkclose()
		cmd := exec.Command("docker", "context", "rm", "-f", name)
		if err1 := cmd.Run(); err == nil {
			err = errors.Wrapf(err1, "failed to remove buildx instance %s", name)
		}
		return err
	}

	return &backend{
		builder:             name,
		context:             name,
		unsupportedFeatures: c.unsupported,
	}, cl, nil
}

func (c dockerWorker) Close() error {
	return nil
}
