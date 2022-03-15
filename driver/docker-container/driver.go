package docker

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/driver/bkimage"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/docker/api/types"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	dockerarchive "github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/idtools"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/tracing/detect"
	"github.com/pkg/errors"
)

const (
	volumeStateSuffix = "_state"
)

type Driver struct {
	driver.InitConfig
	factory      driver.Factory
	userNSRemap  bool // true if dockerd is running with userns-remap mode
	netMode      string
	image        string
	cgroupParent string
	env          []string
}

func (d *Driver) IsMobyDriver() bool {
	return false
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
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
			if err := d.wait(ctx, sub); err != nil {
				return err
			}
			return nil
		})
	})
}

func (d *Driver) create(ctx context.Context, l progress.SubLogger) error {
	imageName := bkimage.DefaultImage
	if d.image != "" {
		imageName = d.image
	}

	if err := l.Wrap("pulling image "+imageName, func() error {
		ra, err := imagetools.RegistryAuthForRef(imageName, d.Auth)
		if err != nil {
			return err
		}
		rc, err := d.DockerAPI.ImageCreate(ctx, imageName, types.ImageCreateOptions{
			RegistryAuth: ra,
		})
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, rc)
		return err
	}); err != nil {
		// image pulling failed, check if it exists in local image store.
		// if not, return pulling error. otherwise log it.
		_, _, errInspect := d.DockerAPI.ImageInspectWithRaw(ctx, imageName)
		if errInspect != nil {
			return err
		}
		l.Wrap("pulling failed, using local image "+imageName, func() error { return nil })
	}

	cfg := &container.Config{
		Image: imageName,
		Env:   d.env,
	}
	if d.InitConfig.BuildkitFlags != nil {
		cfg.Cmd = d.InitConfig.BuildkitFlags
	}

	if err := l.Wrap("creating container "+d.Name, func() error {
		hc := &container.HostConfig{
			Privileged: true,
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeVolume,
					Source: d.Name + volumeStateSuffix,
					Target: confutil.DefaultBuildKitStateDir,
				},
			},
		}
		if d.userNSRemap {
			hc.UsernsMode = "host"
		}
		if d.netMode != "" {
			hc.NetworkMode = container.NetworkMode(d.netMode)
		}
		if info, err := d.DockerAPI.Info(ctx); err == nil && info.CgroupDriver == "cgroupfs" {
			// Place all buildkit containers inside this cgroup by default so limits can be attached
			// to all build activity on the host.
			hc.CgroupParent = "/docker/buildx"
			if d.cgroupParent != "" {
				hc.CgroupParent = d.cgroupParent
			}
		}
		_, err := d.DockerAPI.ContainerCreate(ctx, cfg, hc, &network.NetworkingConfig{}, nil, d.Name)
		if err != nil {
			return err
		}
		if err := d.copyToContainer(ctx, d.InitConfig.Files); err != nil {
			return err
		}
		if err := d.start(ctx, l); err != nil {
			return err
		}
		if err := d.wait(ctx, l); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (d *Driver) wait(ctx context.Context, l progress.SubLogger) error {
	try := 1
	for {
		bufStdout := &bytes.Buffer{}
		bufStderr := &bytes.Buffer{}
		if err := d.run(ctx, []string{"buildctl", "debug", "workers"}, bufStdout, bufStderr); err != nil {
			if try > 15 {
				if err != nil {
					d.copyLogs(context.TODO(), l)
					if bufStdout.Len() != 0 {
						l.Log(1, bufStdout.Bytes())
					}
					if bufStderr.Len() != 0 {
						l.Log(2, bufStderr.Bytes())
					}
				}
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(try*120) * time.Millisecond):
				try++
				continue
			}
		}
		return nil
	}
}

func (d *Driver) copyLogs(ctx context.Context, l progress.SubLogger) error {
	rc, err := d.DockerAPI.ContainerLogs(ctx, d.Name, types.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true,
	})
	if err != nil {
		return err
	}
	stdout := &logWriter{logger: l, stream: 1}
	stderr := &logWriter{logger: l, stream: 2}
	if _, err := stdcopy.StdCopy(stdout, stderr, rc); err != nil {
		return err
	}
	return rc.Close()
}

func (d *Driver) copyToContainer(ctx context.Context, files map[string][]byte) error {
	srcPath, err := writeConfigFiles(files)
	if err != nil {
		return err
	}
	if srcPath != "" {
		defer os.RemoveAll(srcPath)
	}
	srcArchive, err := dockerarchive.TarWithOptions(srcPath, &dockerarchive.TarOptions{
		ChownOpts: &idtools.Identity{UID: 0, GID: 0},
	})
	if err != nil {
		return err
	}
	defer srcArchive.Close()
	return d.DockerAPI.CopyToContainer(ctx, d.Name, "/", srcArchive, dockertypes.CopyToContainerOptions{})
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

func (d *Driver) run(ctx context.Context, cmd []string, stdout, stderr io.Writer) (err error) {
	id, conn, err := d.exec(ctx, cmd)
	if err != nil {
		return err
	}
	if _, err := stdcopy.StdCopy(stdout, stderr, conn); err != nil {
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
	ctn, err := d.DockerAPI.ContainerInspect(ctx, d.Name)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return &driver.Info{
				Status: driver.Inactive,
			}, nil
		}
		return nil, err
	}

	if ctn.State.Running {
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

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	info, err := d.Info(ctx)
	if err != nil {
		return err
	}
	if info.Status != driver.Inactive {
		container, err := d.DockerAPI.ContainerInspect(ctx, d.Name)
		if err != nil {
			return err
		}
		if rmDaemon {
			if err := d.DockerAPI.ContainerRemove(ctx, d.Name, dockertypes.ContainerRemoveOptions{
				RemoveVolumes: true,
				Force:         force,
			}); err != nil {
				return err
			}
			for _, v := range container.Mounts {
				if v.Name != d.Name+volumeStateSuffix {
					continue
				}
				if rmVolume {
					return d.DockerAPI.VolumeRemove(ctx, d.Name+volumeStateSuffix, false)
				}
			}
		}
	}
	return nil
}

func (d *Driver) Client(ctx context.Context) (*client.Client, error) {
	_, conn, err := d.exec(ctx, []string{"buildctl", "dial-stdio"})
	if err != nil {
		return nil, err
	}

	conn = demuxConn(conn)

	exp, err := detect.Exporter()
	if err != nil {
		return nil, err
	}

	td, _ := exp.(client.TracerDelegate)

	return client.New(ctx, "", client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return conn, nil
	}), client.WithTracerDelegate(td))
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
	go stdcopy.StdCopy(pw, os.Stderr, c)
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

type logWriter struct {
	logger progress.SubLogger
	stream int
}

func (l *logWriter) Write(dt []byte) (int, error) {
	l.logger.Log(l.stream, dt)
	return len(dt), nil
}

func writeConfigFiles(m map[string][]byte) (_ string, err error) {
	// Temp dir that will be copied to the container
	tmpDir, err := os.MkdirTemp("", "buildkitd-config")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmpDir)
		}
	}()
	for f, dt := range m {
		f = path.Join(confutil.DefaultBuildKitConfigDir, f)
		p := filepath.Join(tmpDir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, dt, 0600); err != nil {
			return "", err
		}
	}
	return tmpDir, nil
}
