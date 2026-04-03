package workers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/buildx/tests/helpers"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/util/testutil/integration"
	"github.com/pkg/errors"
)

func InitKubernetesWorker() {
	integration.Register(&kubernetesWorker{
		id: "kubernetes",
	})
}

type kubernetesWorker struct {
	id string

	unsupported []string

	k3sConfig string
	k3sClose  func() error
	k3sErr    error
	k3sOnce   sync.Once
}

func (w *kubernetesWorker) Name() string {
	return w.id
}

func (w *kubernetesWorker) Rootless() bool {
	return false
}

func (w *kubernetesWorker) NetNSDetached() bool {
	return false
}

func (w *kubernetesWorker) New(ctx context.Context, cfg *integration.BackendConfig) (integration.Backend, func() error, error) {
	w.k3sOnce.Do(func() {
		w.k3sConfig, w.k3sClose, w.k3sErr = helpers.NewK3sServer(cfg)
	})
	if w.k3sErr != nil {
		return nil, w.k3sClose, w.k3sErr
	}

	cfgfile, release, err := integration.WriteConfig(cfg.DaemonConfig)
	if err != nil {
		return nil, nil, err
	}
	if release != nil {
		defer release()
	}
	defer os.RemoveAll(filepath.Dir(cfgfile))

	name := "integration-kubernetes-" + identity.NewID()
	cmd := exec.CommandContext(ctx, "buildx", "create",
		"--bootstrap",
		"--name="+name,
		"--buildkitd-config="+cfgfile,
		"--driver=kubernetes",
	)
	cmd.Env = append(
		os.Environ(),
		"BUILDX_CONFIG=/tmp/buildx-"+name,
		"KUBECONFIG="+w.k3sConfig,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create buildx instance %s: %s", name, strings.TrimSpace(string(out)))
	}

	cl := func() error {
		cmd := exec.CommandContext(context.Background(), "buildx", "rm", "-f", name)
		cmd.Env = append(
			os.Environ(),
			"BUILDX_CONFIG=/tmp/buildx-"+name,
			"KUBECONFIG="+w.k3sConfig,
		)
		return cmd.Run()
	}

	return &backend{
		builder:             name,
		unsupportedFeatures: w.unsupported,
	}, cl, nil
}

func (w *kubernetesWorker) Close() error {
	if c := w.k3sClose; c != nil {
		if err := c(); err != nil {
			return err
		}
	}

	w.k3sConfig = ""
	w.k3sClose = nil
	w.k3sErr = nil
	w.k3sOnce = sync.Once{}

	return nil
}
