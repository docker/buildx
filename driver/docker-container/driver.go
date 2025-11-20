package docker

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/driver/bkimage"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/ghutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/context/docker"
	"github.com/docker/cli/opts"
	"github.com/moby/buildkit/client"
	mobyarchive "github.com/moby/go-archive"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	dockerclient "github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/jsonmessage"
	"github.com/moby/moby/client/pkg/security"
	"github.com/pkg/errors"
)

const (
	volumeStateSuffix   = "_state"
	buildkitdConfigFile = "buildkitd.toml"
)

type Driver struct {
	driver.InitConfig
	factory driver.Factory

	// if you add fields, remember to update docs:
	// https://github.com/docker/docs/blob/main/content/build/drivers/docker-container.md
	netMode            string
	image              string
	memory             opts.MemBytes
	memorySwap         opts.MemSwapBytes
	cpuQuota           int64
	cpuPeriod          int64
	cpuShares          int64
	cpusetCpus         string
	cpusetMems         string
	cgroupParent       string
	restartPolicy      container.RestartPolicy
	env                []string
	defaultLoad        bool
	gpus               []container.DeviceRequest
	writeProvenanceGHA bool
}

func (d *Driver) IsMobyDriver() bool {
	return false
}

func (d *Driver) Config() driver.InitConfig {
	return d.InitConfig
}

func (d *Driver) Bootstrap(ctx context.Context, l progress.Logger) error {
	return progress.Wrap("[internal] booting buildkit", l, func(sub progress.SubLogger) error {
		_, err := d.DockerAPI.ContainerInspect(ctx, d.Name, dockerclient.ContainerInspectOptions{})
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				return d.create(ctx, sub)
			}
			return err
		}
		return sub.Wrap("starting container "+d.Name, func() error {
			if err := d.start(ctx); err != nil {
				return err
			}
			return d.wait(ctx, sub)
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
		resp, err := d.DockerAPI.ImagePull(ctx, imageName, dockerclient.ImagePullOptions{
			RegistryAuth: ra,
		})
		if err != nil {
			return err
		}
		defer resp.Close()
		return jsonmessage.DisplayJSONMessagesStream(resp, io.Discard, 0, false, nil)
	}); err != nil {
		// image pulling failed, check if it exists in local image store.
		// if not, return pulling error. otherwise log it.
		_, errInspect := d.DockerAPI.ImageInspect(ctx, imageName)
		found := errInspect == nil
		if !found {
			return err
		}
		l.Wrap("pulling failed, using local image "+imageName, func() error { return nil })
	}

	cfg := &container.Config{
		Image: imageName,
		Env:   d.env,
	}
	cfg.Cmd = getBuildkitFlags(d.InitConfig)

	useInit := true // let it cleanup exited processes created by BuildKit's container API
	return l.Wrap("creating container "+d.Name, func() error {
		hc := &container.HostConfig{
			Privileged:    true,
			RestartPolicy: d.restartPolicy,
			Init:          &useInit,
		}

		mounts := []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: d.Name + volumeStateSuffix,
				Target: confutil.DefaultBuildKitStateDir,
			},
		}

		if d.writeProvenanceGHA {
			if ghactx, err := ghutil.GithubActionsContext(); err != nil {
				return err
			} else if ghactx != nil {
				if d.Files == nil {
					d.Files = make(map[string][]byte)
				}
				d.Files["provenance.d/github_actions_context.json"] = ghactx
			}
		}

		// Mount WSL libaries if running in WSL environment and Docker context
		// is a local socket as requesting GPU on container builder creation
		// is not enough when generating the CDI specification for GPU devices.
		// https://github.com/docker/buildx/pull/3320
		if os.Getenv("WSL_DISTRO_NAME") != "" {
			if cm, err := d.ContextStore.GetMetadata(d.DockerContext); err == nil {
				if epm, err := docker.EndpointFromContext(cm); err == nil && isSocket(epm.Host) {
					wslLibPath := "/usr/lib/wsl"
					if st, err := os.Stat(wslLibPath); err == nil && st.IsDir() {
						mounts = append(mounts, mount.Mount{
							Type:     mount.TypeBind,
							Source:   wslLibPath,
							Target:   wslLibPath,
							ReadOnly: true,
						})
					}
				}
			}
		}
		hc.Mounts = mounts

		if d.netMode != "" {
			hc.NetworkMode = container.NetworkMode(d.netMode)
		}
		if d.memory != 0 {
			hc.Memory = int64(d.memory)
		}
		if d.memorySwap != 0 {
			hc.MemorySwap = int64(d.memorySwap)
		}
		if d.cpuQuota != 0 {
			hc.CPUQuota = d.cpuQuota
		}
		if d.cpuPeriod != 0 {
			hc.CPUPeriod = d.cpuPeriod
		}
		if d.cpuShares != 0 {
			hc.CPUShares = d.cpuShares
		}
		if d.cpusetCpus != "" {
			hc.CpusetCpus = d.cpusetCpus
		}
		if d.cpusetMems != "" {
			hc.CpusetMems = d.cpusetMems
		}
		if len(d.gpus) > 0 && d.hasGPUCapability(ctx, cfg.Image, d.gpus) {
			hc.DeviceRequests = d.gpus
		}
		if resp, err := d.DockerAPI.Info(ctx, dockerclient.InfoOptions{}); err == nil {
			if resp.Info.CgroupDriver == "cgroupfs" {
				// Place all buildkit containers inside this cgroup by default so limits can be attached
				// to all build activity on the host.
				hc.CgroupParent = "/docker/buildx"
				if d.cgroupParent != "" {
					hc.CgroupParent = d.cgroupParent
				}
			}

			for _, f := range security.DecodeOptions(resp.Info.SecurityOptions) {
				if f.Name == "userns" {
					hc.UsernsMode = "host"
					break
				}
			}
		}
		_, err := d.DockerAPI.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
			Config:     cfg,
			HostConfig: hc,
			Name:       d.Name,
		})
		if err != nil && !cerrdefs.IsConflict(err) {
			return err
		}
		if err == nil {
			if err := d.copyToContainer(ctx, d.Files); err != nil {
				return err
			}
			if err := d.start(ctx); err != nil {
				return err
			}
		}
		return d.wait(ctx, l)
	})
}

func (d *Driver) wait(ctx context.Context, l progress.SubLogger) error {
	try := 1
	for {
		bufStdout := &bytes.Buffer{}
		bufStderr := &bytes.Buffer{}
		if err := d.run(ctx, []string{"buildctl", "debug", "workers"}, bufStdout, bufStderr); err != nil {
			if try > 15 {
				d.copyLogs(context.TODO(), l)
				if bufStdout.Len() != 0 {
					l.Log(1, bufStdout.Bytes())
				}
				if bufStderr.Len() != 0 {
					l.Log(2, bufStderr.Bytes())
				}
				return err
			}
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			case <-time.After(time.Duration(try*120) * time.Millisecond):
				try++
				continue
			}
		}
		return nil
	}
}

func (d *Driver) copyLogs(ctx context.Context, l progress.SubLogger) error {
	rc, err := d.DockerAPI.ContainerLogs(ctx, d.Name, dockerclient.ContainerLogsOptions{
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
	srcArchive, err := mobyarchive.TarWithOptions(srcPath, &mobyarchive.TarOptions{
		ChownOpts: &mobyarchive.ChownOpts{UID: 0, GID: 0},
	})
	if err != nil {
		return err
	}
	defer srcArchive.Close()

	_, err = d.DockerAPI.CopyToContainer(ctx, d.Name, dockerclient.CopyToContainerOptions{
		DestinationPath: path.Dir(confutil.DefaultBuildKitConfigDir),
		Content:         srcArchive,
	})
	return err
}

func (d *Driver) exec(ctx context.Context, cmd []string) (string, net.Conn, error) {
	response, err := d.DockerAPI.ExecCreate(ctx, d.Name, dockerclient.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", nil, err
	}

	execID := response.ID
	if execID == "" {
		return "", nil, errors.New("exec ID empty")
	}

	resp, err := d.DockerAPI.ExecAttach(ctx, execID, dockerclient.ExecAttachOptions{})
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
	resp, err := d.DockerAPI.ExecInspect(ctx, id, dockerclient.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if resp.ExitCode != 0 {
		return errors.Errorf("exit code %d", resp.ExitCode)
	}
	return nil
}

func (d *Driver) start(ctx context.Context) error {
	_, err := d.DockerAPI.ContainerStart(ctx, d.Name, dockerclient.ContainerStartOptions{})
	return err
}

func (d *Driver) Info(ctx context.Context) (*driver.Info, error) {
	res, err := d.DockerAPI.ContainerInspect(ctx, d.Name, dockerclient.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return &driver.Info{
				Status: driver.Inactive,
			}, nil
		}
		return nil, err
	}

	if res.Container.State.Running {
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
		_, err = d.DockerAPI.ContainerStop(ctx, d.Name, dockerclient.ContainerStopOptions{})
		return err
	}
	return nil
}

func (d *Driver) Rm(ctx context.Context, force, rmVolume, rmDaemon bool) error {
	info, err := d.Info(ctx)
	if err != nil {
		return err
	}
	if info.Status != driver.Inactive {
		res, err := d.DockerAPI.ContainerInspect(ctx, d.Name, dockerclient.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		if rmDaemon {
			if _, err := d.DockerAPI.ContainerRemove(ctx, d.Name, dockerclient.ContainerRemoveOptions{
				RemoveVolumes: true,
				Force:         force,
			}); err != nil {
				return err
			}
			if rmVolume {
				for _, v := range res.Container.Mounts {
					if v.Name == d.Name+volumeStateSuffix {
						_, err = d.DockerAPI.VolumeRemove(ctx, v.Name, dockerclient.VolumeRemoveOptions{})
						return err
					}
				}
			}
		}
	}
	return nil
}

func (d *Driver) Dial(ctx context.Context) (net.Conn, error) {
	_, conn, err := d.exec(ctx, []string{"buildctl", "dial-stdio"})
	if err != nil {
		return nil, err
	}
	conn = demuxConn(conn)
	return conn, nil
}

func (d *Driver) Client(ctx context.Context, opts ...client.ClientOpt) (*client.Client, error) {
	conn, err := d.Dial(ctx)
	if err != nil {
		return nil, err
	}

	var counter int64
	opts = append([]client.ClientOpt{
		client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			if atomic.AddInt64(&counter, 1) > 1 {
				return nil, net.ErrClosed
			}
			return conn, nil
		}),
	}, opts...)
	return client.New(ctx, "", opts...)
}

func (d *Driver) Factory() driver.Factory {
	return d.factory
}

func (d *Driver) Features(ctx context.Context) map[driver.Feature]bool {
	return map[driver.Feature]bool{
		driver.OCIExporter:    true,
		driver.DockerExporter: true,
		driver.CacheExport:    true,
		driver.MultiPlatform:  true,
		driver.DirectPush:     true,
		driver.DefaultLoad:    d.defaultLoad,
	}
}

func (d *Driver) HostGatewayIP(ctx context.Context) (net.IP, error) {
	return nil, errors.New("host-gateway is not supported by the docker-container driver")
}

// hasGPUCapability checks if docker daemon has GPU capability. We need to run
// a dummy container with GPU device to check if the daemon has this capability
// because there is no API to check it yet.
func (d *Driver) hasGPUCapability(ctx context.Context, image string, gpus []container.DeviceRequest) bool {
	resp, err := d.DockerAPI.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &container.Config{
			Image:      image,
			Entrypoint: []string{"/bin/true"},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode(container.IPCModeNone),
			AutoRemove:  true,
			Resources: container.Resources{
				DeviceRequests: gpus,
			},
		},
	})
	if err != nil {
		return false
	}
	if _, err := d.DockerAPI.ContainerStart(ctx, resp.ID, dockerclient.ContainerStartOptions{}); err != nil {
		return false
	}
	return true
}

func demuxConn(c net.Conn) net.Conn {
	pr, pw := io.Pipe()
	// TODO: rewrite parser with Reader() to avoid goroutine switch
	go func() {
		_, err := stdcopy.StdCopy(pw, os.Stderr, c)
		pw.CloseWithError(err)
	}()
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
	configDir := filepath.Base(confutil.DefaultBuildKitConfigDir)
	for f, dt := range m {
		p := filepath.Join(tmpDir, configDir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, dt, 0644); err != nil {
			return "", err
		}
	}
	return tmpDir, nil
}

func getBuildkitFlags(initConfig driver.InitConfig) []string {
	flags := initConfig.BuildkitdFlags
	if _, ok := initConfig.Files[buildkitdConfigFile]; ok {
		// There's no way for us to determine the appropriate default configuration
		// path and the default path can vary depending on if the image is normal
		// or rootless.
		//
		// In order to ensure that --config works, copy to a specific path and
		// specify the location.
		//
		// This should be appended before the user-specified arguments
		// so that this option could be overwritten by the user.
		newFlags := make([]string, 0, len(flags)+2)
		newFlags = append(newFlags, "--config", path.Join("/etc/buildkit", buildkitdConfigFile))
		flags = append(newFlags, flags...)
	}
	return flags
}

func isSocket(addr string) bool {
	switch proto, _, _ := strings.Cut(addr, "://"); proto {
	case "unix", "npipe", "fd":
		return true
	default:
		return false
	}
}
