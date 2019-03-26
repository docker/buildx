package docker

import (
	"context"
	"io"
	"io/ioutil"

	"github.com/docker/docker/api/types"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/tonistiigi/buildx/driver"
)

type Driver struct {
	driver.InitConfig
	version dockertypes.Version
}

func (d *Driver) Bootstrap(ctx context.Context, l driver.Logger) error {
	_, err := d.DockerAPI.ContainerInspect(ctx, d.Name)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return d.create(ctx, l)
		}
		return err
	}
	return d.start(ctx, l)
}

func (d *Driver) create(ctx context.Context, l driver.Logger) error {
	rc, err := d.DockerAPI.ImageCreate(ctx, "moby/buildkit", types.ImageCreateOptions{})
	if err != nil {
		return err
	}
	_, err = io.Copy(ioutil.Discard, rc)
	if err != nil {
		return err
	}
	_, err = d.DockerAPI.ContainerCreate(ctx, &container.Config{
		Image: "moby/buildkit",
	}, &container.HostConfig{
		Privileged: true,
	}, &network.NetworkingConfig{}, d.Name)
	if err != nil {
		return err
	}
	if err := d.start(ctx, l); err != nil {
		return err
	}
	return nil
}

func (d *Driver) start(ctx context.Context, l driver.Logger) error {
	return d.DockerAPI.ContainerStart(ctx, d.Name, types.ContainerStartOptions{})
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	container, err := d.DockerAPI.ContainerInspect(ctx, d.Name)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return &driver.Info{
				Status: driver.Terminated,
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
	return errors.Errorf("stop not implemented for %T", d)
}

func (d *Driver) Rm(ctx context.Context, force bool) error {
	return errors.Errorf("rm not implemented for %T", d)
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	return nil, errors.Errorf("client not implemented for %T", d)
}
