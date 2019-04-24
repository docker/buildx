package docker

import (
	"context"
	"io"
	"io/ioutil"
	"net"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
	"github.com/tonistiigi/buildx/util/progress"
)

var buildkitImage = "moby/buildkit:master" // TODO: make this verified and configuratble

type Driver struct {
	driver.InitConfig
	factory driver.Factory
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return progress.Wrap("[internal] booting buildkit", l, func(sub progress.SubLogger) error {
		_, err := d.DockerAPI.ContainerInspect(ctx, d.Name)
		if err != nil {
			if dockerclient.IsErrNotFound(err) {
				return d.create(ctx, sub)
			}
			return err
		}
		return sub.Wrap("starting container "+d.Name, func() error {
			if err := d.start(ctx, sub); err != nil {
				return err
			}
			if err := d.wait(ctx); err != nil {
				return err
			}
			return nil
		})
	})
}

func (d *Driver) create(ctx context.Context, l progress.SubLogger) error {
	if err := l.Wrap("pulling image "+buildkitImage, func() error {
		rc, err := d.DockerAPI.ImageCreate(ctx, buildkitImage, types.ImageCreateOptions{})
		if err != nil {
			return err
		}
		_, err = io.Copy(ioutil.Discard, rc)
		return err
	}); err != nil {
		return err
	}
	if err := l.Wrap("creating container "+d.Name, func() error {
		_, err := d.DockerAPI.ContainerCreate(ctx, &container.Config{
			Image: buildkitImage,
		}, &container.HostConfig{
			Privileged: true,
		}, &network.NetworkingConfig{}, d.Name)
		if err != nil {
			return err
		}
		if err := d.start(ctx, l); err != nil {
			return err
		}
		if err := d.wait(ctx); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (d *Driver) wait(ctx context.Context) error {
	try := 0
	for {
		if err := d.run(ctx, []string{"buildctl", "debug", "workers"}); err != nil {
			if try > 10 {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(100+try*20) * time.Millisecond):
				try++
				continue
			}
		}
		return nil
	}
}

func (d *Driver) exec(ctx context.Context, cmd []string) (string, net.Conn, error) {
	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}
	response, err := d.DockerAPI.ContainerExecCreate(ctx, d.Name, execConfig)
	if err != nil {
		return "", nil, err
	}

	execID := response.ID
	if execID == "" {
		return "", nil, errors.New("exec ID empty")
	}

	resp, err := d.DockerAPI.ContainerExecAttach(ctx, execID, types.ExecStartCheck{})
	if err != nil {
		return "", nil, err
	}
	return execID, resp.Conn, nil
}

func (d *Driver) run(ctx context.Context, cmd []string) error {
	id, conn, err := d.exec(ctx, cmd)
	if err != nil {
		return err
	}
	if _, err := io.Copy(ioutil.Discard, conn); err != nil {
		return err
	}
	conn.Close()
	resp, err := d.DockerAPI.ContainerExecInspect(ctx, id)
	if err != nil {
		return err
	}
	if resp.ExitCode != 0 {
		return errors.Errorf("exit code %d", resp.ExitCode)
	}
	return nil
}

func (d *Driver) start(ctx context.Context, l progress.SubLogger) error {
	return d.DockerAPI.ContainerStart(ctx, d.Name, types.ContainerStartOptions{})
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	container, err := d.DockerAPI.ContainerInspect(ctx, d.Name)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return &driver.Info{
				Status: driver.Inactive,
			}, nil
		}
		return nil, err
	}

	if container.State.Running {
		return &driver.Info{
			Status: driver.Running,
		}, nil
	}

	return &driver.Info{
		Status: driver.Stopped,
	}, nil
}

func (d *Driver) Stop(ctx context.Context, force bool) error {
	info, err := d.Info(ctx)
	if err != nil {
		return err
	}
	if info.Status == driver.Running {
		return d.DockerAPI.ContainerStop(ctx, d.Name, nil)
	}
	return nil
}

func (d *Driver) Rm(ctx context.Context, force bool) error {
	info, err := d.Info(ctx)
	if err != nil {
		return err
	}
	if info.Status != driver.Inactive {
		return d.DockerAPI.ContainerRemove(ctx, d.Name, dockertypes.ContainerRemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		})
	}
	return nil
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	_, conn, err := d.exec(ctx, []string{"buildctl", "dial-stdio"})
	if err != nil {
		return nil, err
	}

	conn = demuxConn(conn)

	return client.New(ctx, "", client.WithDialer(func(string, time.Duration) (net.Conn, error) {
		return conn, nil
	}))
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) Features() map[driver.Feature]bool {
	return map[driver.Feature]bool{
		driver.OCIExporter:    true,
		driver.DockerExporter: true,

		driver.CacheExport:   true,
		driver.MultiPlatform: true,
	}
}

func demuxConn(c net.Conn) net.Conn {
	pr, pw := io.Pipe()
	// TODO: rewrite parser with Reader() to avoid goroutine switch
	go stdcopy.StdCopy(pw, os.Stdout, c)
	return &demux{
		Conn:   c,
		Reader: pr,
	}
}

type demux struct {
	net.Conn
	io.Reader
}

func (d *demux) Read(dt []byte) (int, error) {
	return d.Reader.Read(dt)
}
