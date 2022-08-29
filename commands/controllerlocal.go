package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/containerd/console"
	"github.com/docker/buildx/build"
	controllerapi "github.com/docker/buildx/commands/controller/pb"
	"github.com/docker/buildx/monitor"
	"github.com/docker/cli/cli/command"
)

func newLocalBuildxController(ctx context.Context, dockerCli command.Cli) monitor.BuildxController {
	return &localController{
		dockerCli: dockerCli,
		ref:       "local",
	}
}

type localController struct {
	dockerCli command.Cli
	ref       string
	resultCtx *build.ResultContext
}

func (b *localController) Invoke(ctx context.Context, ref string, cfg controllerapi.ContainerConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error {
	if ref != b.ref {
		return fmt.Errorf("unknown ref %q", ref)
	}
	if b.resultCtx == nil {
		return fmt.Errorf("no build result is registered")
	}
	ccfg := build.ContainerConfig{
		ResultCtx:  b.resultCtx,
		Entrypoint: cfg.Entrypoint,
		Cmd:        cfg.Cmd,
		Env:        cfg.Env,
		Tty:        cfg.Tty,
		Stdin:      ioIn,
		Stdout:     ioOut,
		Stderr:     ioErr,
	}
	if !cfg.NoUser {
		ccfg.User = &cfg.User
	}
	if !cfg.NoCwd {
		ccfg.Cwd = &cfg.Cwd
	}
	return build.Invoke(ctx, ccfg)
}

func (b *localController) Build(ctx context.Context, options controllerapi.BuildOptions, in io.ReadCloser, w io.Writer, out console.File, progressMode string) (string, error) {
	res, err := runBuildWithContext(ctx, b.dockerCli, options, in, progressMode, nil)
	if err != nil {
		return "", err
	}
	b.resultCtx = res
	return b.ref, nil
}

func (b *localController) Kill(context.Context) error {
	return nil // nop
}

func (b *localController) Close() error {
	// TODO: cancel current build and invoke
	return nil
}

func (b *localController) List(ctx context.Context) (res []string, _ error) {
	return []string{b.ref}, nil
}

func (b *localController) Disconnect(ctx context.Context, key string) error {
	return nil // nop
}
