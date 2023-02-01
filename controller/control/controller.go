package control

import (
	"context"
	"io"

	"github.com/containerd/console"
	controllerapi "github.com/docker/buildx/controller/pb"
)

type BuildxController interface {
	Invoke(ctx context.Context, ref string, options controllerapi.ContainerConfig, ioIn io.ReadCloser, ioOut io.WriteCloser, ioErr io.WriteCloser) error
	Build(ctx context.Context, options controllerapi.BuildOptions, in io.ReadCloser, w io.Writer, out console.File, progressMode string) (ref string, err error)
	Kill(ctx context.Context) error
	Close() error
	List(ctx context.Context) (res []string, _ error)
	Disconnect(ctx context.Context, ref string) error
}

type ControlOptions struct {
	ServerConfig string
	Root         string
	Detach       bool
}
