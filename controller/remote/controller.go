//go:build linux

package remote

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/containerd/log"
	"github.com/docker/buildx/build"
	cbuild "github.com/docker/buildx/controller/build"
	"github.com/docker/buildx/controller/control"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/version"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

const (
	serveCommandName = "_INTERNAL_SERVE"
)

var (
	defaultLogFilename    = fmt.Sprintf("buildx.%s.log", version.Revision)
	defaultSocketFilename = fmt.Sprintf("buildx.%s.sock", version.Revision)
	defaultPIDFilename    = fmt.Sprintf("buildx.%s.pid", version.Revision)
)

type serverConfig struct {
	// Specify buildx server root
	Root string `toml:"root"`

	// LogLevel sets the logging level [trace, debug, info, warn, error, fatal, panic]
	LogLevel string `toml:"log_level"`

	// Specify file to output buildx server log
	LogFile string `toml:"log_file"`
}

func NewRemoteBuildxController(ctx context.Context, dockerCli command.Cli, opts control.ControlOptions, logger progress.SubLogger) (control.BuildxController, error) {
	rootDir := opts.Root
	if rootDir == "" {
		rootDir = rootDataDir(dockerCli)
	}
	serverRoot := filepath.Join(rootDir, "shared")

	// connect to buildx server if it is already running
	ctx2, cancel := context.WithCancelCause(ctx)
	ctx2, _ = context.WithTimeoutCause(ctx2, 1*time.Second, errors.WithStack(context.DeadlineExceeded)) //nolint:govet,lostcancel // no need to manually cancel this context as we already rely on parent
	c, err := newBuildxClientAndCheck(ctx2, filepath.Join(serverRoot, defaultSocketFilename))
	cancel(errors.WithStack(context.Canceled))
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.Wrap(err, "cannot connect to the buildx server")
		}
	} else {
		return &buildxController{c, serverRoot}, nil
	}

	// start buildx server via subcommand
	err = logger.Wrap("no buildx server found; launching...", func() error {
		launchFlags := []string{}
		if opts.ServerConfig != "" {
			launchFlags = append(launchFlags, "--config", opts.ServerConfig)
		}
		logFile, err := getLogFilePath(dockerCli, opts.ServerConfig)
		if err != nil {
			return err
		}
		wait, err := launch(ctx, logFile, append([]string{serveCommandName}, launchFlags...)...)
		if err != nil {
			return err
		}
		go wait()

		// wait for buildx server to be ready
		ctx2, cancel = context.WithCancelCause(ctx)
		ctx2, _ = context.WithTimeoutCause(ctx2, 10*time.Second, errors.WithStack(context.DeadlineExceeded)) //nolint:govet,lostcancel // no need to manually cancel this context as we already rely on parent
		c, err = newBuildxClientAndCheck(ctx2, filepath.Join(serverRoot, defaultSocketFilename))
		cancel(errors.WithStack(context.Canceled))
		if err != nil {
			return errors.Wrap(err, "cannot connect to the buildx server")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &buildxController{c, serverRoot}, nil
}

func AddControllerCommands(cmd *cobra.Command, dockerCli command.Cli) {
	cmd.AddCommand(
		serveCmd(dockerCli),
	)
}

func serveCmd(dockerCli command.Cli) *cobra.Command {
	var serverConfigPath string
	cmd := &cobra.Command{
		Use:    fmt.Sprintf("%s [OPTIONS]", serveCommandName),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse config
			config, err := getConfig(dockerCli, serverConfigPath)
			if err != nil {
				return err
			}
			if config.LogLevel == "" {
				logrus.SetLevel(logrus.InfoLevel)
			} else {
				lvl, err := logrus.ParseLevel(config.LogLevel)
				if err != nil {
					return errors.Wrap(err, "failed to prepare logger")
				}
				logrus.SetLevel(lvl)
			}
			logrus.SetFormatter(&logrus.JSONFormatter{
				TimestampFormat: log.RFC3339NanoFixed,
			})
			root, err := prepareRootDir(dockerCli, config)
			if err != nil {
				return err
			}
			pidF := filepath.Join(root, defaultPIDFilename)
			if err := os.WriteFile(pidF, []byte(fmt.Sprintf("%d", os.Getpid())), 0600); err != nil {
				return err
			}
			defer func() {
				if err := os.Remove(pidF); err != nil {
					logrus.Errorf("failed to clean up info file %q: %v", pidF, err)
				}
			}()

			// prepare server
			b := NewServer(func(ctx context.Context, options *controllerapi.BuildOptions, stdin io.Reader, progress progress.Writer) (*client.SolveResponse, *build.ResultHandle, *build.Inputs, error) {
				return cbuild.RunBuild(ctx, dockerCli, options, stdin, progress, true)
			})
			defer b.Close()

			// serve server
			addr := filepath.Join(root, defaultSocketFilename)
			if err := os.Remove(addr); err != nil && !os.IsNotExist(err) { // avoid EADDRINUSE
				return err
			}
			defer func() {
				if err := os.Remove(addr); err != nil {
					logrus.Errorf("failed to clean up socket %q: %v", addr, err)
				}
			}()
			logrus.Infof("starting server at %q", addr)
			l, err := net.Listen("unix", addr)
			if err != nil {
				return err
			}
			rpc := grpc.NewServer(
				grpc.UnaryInterceptor(grpcerrors.UnaryServerInterceptor),
				grpc.StreamInterceptor(grpcerrors.StreamServerInterceptor),
			)
			controllerapi.RegisterControllerServer(rpc, b)
			doneCh := make(chan struct{})
			errCh := make(chan error, 1)
			go func() {
				defer close(doneCh)
				if err := rpc.Serve(l); err != nil {
					errCh <- errors.Wrapf(err, "error on serving via socket %q", addr)
				}
			}()

			var s os.Signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT)
			signal.Notify(sigCh, syscall.SIGTERM)
			select {
			case err := <-errCh:
				logrus.Errorf("got error %s, exiting", err)
				return err
			case s = <-sigCh:
				logrus.Infof("got signal %s, exiting", s)
				return nil
			case <-doneCh:
				logrus.Infof("rpc server done, exiting")
				return nil
			}
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&serverConfigPath, "config", "", "Specify buildx server config file")
	return cmd
}

func getLogFilePath(dockerCli command.Cli, configPath string) (string, error) {
	config, err := getConfig(dockerCli, configPath)
	if err != nil {
		return "", err
	}
	if config.LogFile == "" {
		root, err := prepareRootDir(dockerCli, config)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, defaultLogFilename), nil
	}
	return config.LogFile, nil
}

func getConfig(dockerCli command.Cli, configPath string) (*serverConfig, error) {
	var defaultConfigPath bool
	if configPath == "" {
		defaultRoot := rootDataDir(dockerCli)
		configPath = filepath.Join(defaultRoot, "config.toml")
		defaultConfigPath = true
	}
	var config serverConfig
	tree, err := toml.LoadFile(configPath)
	if err != nil && !(os.IsNotExist(err) && defaultConfigPath) {
		return nil, errors.Wrapf(err, "failed to read config %q", configPath)
	} else if err == nil {
		if err := tree.Unmarshal(&config); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal config %q", configPath)
		}
	}
	return &config, nil
}

func prepareRootDir(dockerCli command.Cli, config *serverConfig) (string, error) {
	rootDir := config.Root
	if rootDir == "" {
		rootDir = rootDataDir(dockerCli)
	}
	if rootDir == "" {
		return "", errors.New("buildx root dir must be determined")
	}
	if err := os.MkdirAll(rootDir, 0700); err != nil {
		return "", err
	}
	serverRoot := filepath.Join(rootDir, "shared")
	if err := os.MkdirAll(serverRoot, 0700); err != nil {
		return "", err
	}
	return serverRoot, nil
}

func rootDataDir(dockerCli command.Cli) string {
	return filepath.Join(confutil.NewConfig(dockerCli).Dir(), "controller")
}

func newBuildxClientAndCheck(ctx context.Context, addr string) (*Client, error) {
	c, err := NewClient(ctx, addr)
	if err != nil {
		return nil, err
	}
	p, v, r, err := c.Version(ctx)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("connected to server (\"%v %v %v\")", p, v, r)
	if !(p == version.Package && v == version.Version && r == version.Revision) {
		return nil, errors.Errorf("version mismatch (client: \"%v %v %v\", server: \"%v %v %v\")", version.Package, version.Version, version.Revision, p, v, r)
	}
	return c, nil
}

type buildxController struct {
	*Client
	serverRoot string
}

func (c *buildxController) Kill(ctx context.Context) error {
	pidB, err := os.ReadFile(filepath.Join(c.serverRoot, defaultPIDFilename))
	if err != nil {
		return err
	}
	pid, err := strconv.ParseInt(string(pidB), 10, 64)
	if err != nil {
		return err
	}
	if pid <= 0 {
		return errors.New("no PID is recorded for buildx server")
	}
	p, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGINT); err != nil {
		return err
	}
	// TODO: Should we send SIGKILL if process doesn't finish?
	return nil
}

func launch(ctx context.Context, logFile string, args ...string) (func() error, error) {
	// set absolute path of binary, since we set the working directory to the root
	pathname, err := os.Executable()
	if err != nil {
		return nil, err
	}
	bCmd := exec.CommandContext(ctx, pathname, args...)
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		bCmd.Stdout = f
		bCmd.Stderr = f
	}
	bCmd.Stdin = nil
	bCmd.Dir = "/"
	bCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	if err := bCmd.Start(); err != nil {
		return nil, err
	}
	return bCmd.Wait, nil
}
