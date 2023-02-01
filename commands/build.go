package commands

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/containerd/console"
	"github.com/docker/buildx/controller"
	cbuild "github.com/docker/buildx/controller/build"
	"github.com/docker/buildx/controller/control"
	controllerapi "github.com/docker/buildx/controller/pb"
	"github.com/docker/buildx/monitor"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/ioset"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli-docs-tool/annotation"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	dockeropts "github.com/docker/cli/opts"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc/codes"
)

type buildOptions struct {
	progress string
	invoke   string
	controllerapi.BuildOptions
	control.ControlOptions
}

func runBuild(dockerCli command.Cli, in buildOptions) error {
	ctx := appcontext.Context()

	ctx, end, err := tracing.TraceCurrentCommand(ctx, "build")
	if err != nil {
		return err
	}
	defer func() {
		end(err)
	}()

	_, err = cbuild.RunBuild(ctx, dockerCli, in.BuildOptions, os.Stdin, in.progress, nil)
	return err
}

func buildCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	options := newBuildOptions()
	cFlags := &commonFlags{}

	cmd := &cobra.Command{
		Use:     "build [OPTIONS] PATH | URL | -",
		Aliases: []string{"b"},
		Short:   "Start a build",
		Args:    cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.ContextPath = args[0]
			options.Opts.Builder = rootOpts.builder
			options.Opts.MetadataFile = cFlags.metadataFile
			options.Opts.NoCache = false
			if cFlags.noCache != nil {
				options.Opts.NoCache = *cFlags.noCache
			}
			options.Opts.Pull = false
			if cFlags.pull != nil {
				options.Opts.Pull = *cFlags.pull
			}
			options.progress = cFlags.progress
			cmd.Flags().VisitAll(checkWarnedFlags)
			if isExperimental() {
				return launchControllerAndRunBuild(dockerCli, options)
			}
			return runBuild(dockerCli, options)
		},
	}

	var platformsDefault []string
	if v := os.Getenv("DOCKER_DEFAULT_PLATFORM"); v != "" {
		platformsDefault = []string{v}
	}

	flags := cmd.Flags()

	flags.StringSliceVar(&options.ExtraHosts, "add-host", []string{}, `Add a custom host-to-IP mapping (format: "host:ip")`)
	flags.SetAnnotation("add-host", annotation.ExternalURL, []string{"https://docs.docker.com/engine/reference/commandline/build/#add-host"})

	flags.StringSliceVar(&options.Allow, "allow", []string{}, `Allow extra privileged entitlement (e.g., "network.host", "security.insecure")`)

	flags.StringArrayVar(&options.BuildArgs, "build-arg", []string{}, "Set build-time variables")

	flags.StringArrayVar(&options.CacheFrom, "cache-from", []string{}, `External cache sources (e.g., "user/app:cache", "type=local,src=path/to/dir")`)

	flags.StringArrayVar(&options.CacheTo, "cache-to", []string{}, `Cache export destinations (e.g., "user/app:cache", "type=local,dest=path/to/dir")`)

	flags.StringVar(&options.CgroupParent, "cgroup-parent", "", "Optional parent cgroup for the container")
	flags.SetAnnotation("cgroup-parent", annotation.ExternalURL, []string{"https://docs.docker.com/engine/reference/commandline/build/#cgroup-parent"})

	flags.StringArrayVar(&options.Contexts, "build-context", []string{}, "Additional build contexts (e.g., name=path)")

	flags.StringVarP(&options.DockerfileName, "file", "f", "", `Name of the Dockerfile (default: "PATH/Dockerfile")`)
	flags.SetAnnotation("file", annotation.ExternalURL, []string{"https://docs.docker.com/engine/reference/commandline/build/#file"})

	flags.StringVar(&options.ImageIDFile, "iidfile", "", "Write the image ID to the file")

	flags.StringArrayVar(&options.Labels, "label", []string{}, "Set metadata for an image")

	flags.BoolVar(&options.Opts.ExportLoad, "load", false, `Shorthand for "--output=type=docker"`)

	flags.StringVar(&options.NetworkMode, "network", "default", `Set the networking mode for the "RUN" instructions during build`)

	flags.StringArrayVar(&options.NoCacheFilter, "no-cache-filter", []string{}, "Do not cache specified stages")

	flags.StringArrayVarP(&options.Outputs, "output", "o", []string{}, `Output destination (format: "type=local,dest=path")`)

	flags.StringArrayVar(&options.Platforms, "platform", platformsDefault, "Set target platform for build")

	if isExperimental() {
		flags.StringVar(&options.PrintFunc, "print", "", "Print result of information request (e.g., outline, targets) [experimental]")
	}

	flags.BoolVar(&options.Opts.ExportPush, "push", false, `Shorthand for "--output=type=registry"`)

	flags.BoolVarP(&options.Quiet, "quiet", "q", false, "Suppress the build output and print image ID on success")

	flags.StringArrayVar(&options.Secrets, "secret", []string{}, `Secret to expose to the build (format: "id=mysecret[,src=/local/secret]")`)

	flags.Var(newShmSize(&options), "shm-size", `Size of "/dev/shm"`)

	flags.StringArrayVar(&options.SSH, "ssh", []string{}, `SSH agent socket or keys to expose to the build (format: "default|<id>[=<socket>|<key>[,<key>]]")`)

	flags.StringArrayVarP(&options.Tags, "tag", "t", []string{}, `Name and optionally a tag (format: "name:tag")`)
	flags.SetAnnotation("tag", annotation.ExternalURL, []string{"https://docs.docker.com/engine/reference/commandline/build/#tag"})

	flags.StringVar(&options.Target, "target", "", "Set the target build stage to build")
	flags.SetAnnotation("target", annotation.ExternalURL, []string{"https://docs.docker.com/engine/reference/commandline/build/#target"})

	flags.Var(newUlimits(&options), "ulimit", "Ulimit options")

	flags.StringArrayVar(&options.Attests, "attest", []string{}, `Attestation parameters (format: "type=sbom,generator=image")`)
	flags.StringVar(&options.Opts.SBOM, "sbom", "", `Shorthand for "--attest=type=sbom"`)
	flags.StringVar(&options.Opts.Provenance, "provenance", "", `Shortand for "--attest=type=provenance"`)

	if isExperimental() {
		flags.StringVar(&options.invoke, "invoke", "", "Invoke a command after the build [experimental]")
		flags.StringVar(&options.Root, "root", "", "Specify root directory of server to connect [experimental]")
		flags.BoolVar(&options.Detach, "detach", runtime.GOOS == "linux", "Detach buildx server (supported only on linux) [experimental]")
		flags.StringVar(&options.ServerConfig, "server-config", "", "Specify buildx server config file (used only when launching new server) [experimental]")
	}

	// hidden flags
	var ignore string
	var ignoreSlice []string
	var ignoreBool bool
	var ignoreInt int64

	flags.BoolVar(&ignoreBool, "compress", false, "Compress the build context using gzip")
	flags.MarkHidden("compress")

	flags.StringVar(&ignore, "isolation", "", "Container isolation technology")
	flags.MarkHidden("isolation")
	flags.SetAnnotation("isolation", "flag-warn", []string{"isolation flag is deprecated with BuildKit."})

	flags.StringSliceVar(&ignoreSlice, "security-opt", []string{}, "Security options")
	flags.MarkHidden("security-opt")
	flags.SetAnnotation("security-opt", "flag-warn", []string{`security-opt flag is deprecated. "RUN --security=insecure" should be used with BuildKit.`})

	flags.BoolVar(&ignoreBool, "squash", false, "Squash newly built layers into a single new layer")
	flags.MarkHidden("squash")
	flags.SetAnnotation("squash", "flag-warn", []string{"experimental flag squash is removed with BuildKit. You should squash inside build using a multi-stage Dockerfile for efficiency."})

	flags.StringVarP(&ignore, "memory", "m", "", "Memory limit")
	flags.MarkHidden("memory")

	flags.StringVar(&ignore, "memory-swap", "", `Swap limit equal to memory plus swap: "-1" to enable unlimited swap`)
	flags.MarkHidden("memory-swap")

	flags.Int64VarP(&ignoreInt, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	flags.MarkHidden("cpu-shares")

	flags.Int64Var(&ignoreInt, "cpu-period", 0, "Limit the CPU CFS (Completely Fair Scheduler) period")
	flags.MarkHidden("cpu-period")

	flags.Int64Var(&ignoreInt, "cpu-quota", 0, "Limit the CPU CFS (Completely Fair Scheduler) quota")
	flags.MarkHidden("cpu-quota")

	flags.StringVar(&ignore, "cpuset-cpus", "", `CPUs in which to allow execution ("0-3", "0,1")`)
	flags.MarkHidden("cpuset-cpus")

	flags.StringVar(&ignore, "cpuset-mems", "", `MEMs in which to allow execution ("0-3", "0,1")`)
	flags.MarkHidden("cpuset-mems")

	flags.BoolVar(&ignoreBool, "rm", true, "Remove intermediate containers after a successful build")
	flags.MarkHidden("rm")

	flags.BoolVar(&ignoreBool, "force-rm", false, "Always remove intermediate containers")
	flags.MarkHidden("force-rm")

	commonBuildFlags(cFlags, flags)
	return cmd
}

// comomnFlags is a set of flags commonly shared among subcommands.
type commonFlags struct {
	metadataFile string
	progress     string
	noCache      *bool
	pull         *bool
}

func commonBuildFlags(options *commonFlags, flags *pflag.FlagSet) {
	options.noCache = flags.Bool("no-cache", false, "Do not use cache when building the image")
	flags.StringVar(&options.progress, "progress", "auto", `Set type of progress output ("auto", "plain", "tty"). Use plain to show container output`)
	options.pull = flags.Bool("pull", false, "Always attempt to pull all referenced images")
	flags.StringVar(&options.metadataFile, "metadata-file", "", "Write build result metadata to the file")
}

func checkWarnedFlags(f *pflag.Flag) {
	if !f.Changed {
		return
	}
	for t, m := range f.Annotations {
		switch t {
		case "flag-warn":
			logrus.Warn(m[0])
		}
	}
}

func writeMetadataFile(filename string, dt interface{}) error {
	b, err := json.MarshalIndent(dt, "", "  ")
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(filename, b, 0644)
}

func decodeExporterResponse(exporterResponse map[string]string) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range exporterResponse {
		dt, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			out[k] = v
			continue
		}
		var raw map[string]interface{}
		if err = json.Unmarshal(dt, &raw); err != nil || len(raw) == 0 {
			out[k] = v
			continue
		}
		out[k] = json.RawMessage(dt)
	}
	return out
}

func wrapBuildError(err error, bake bool) error {
	if err == nil {
		return nil
	}
	st, ok := grpcerrors.AsGRPCStatus(err)
	if ok {
		if st.Code() == codes.Unimplemented && strings.Contains(st.Message(), "unsupported frontend capability moby.buildkit.frontend.contexts") {
			msg := "current frontend does not support --build-context."
			if bake {
				msg = "current frontend does not support defining additional contexts for targets."
			}
			msg += " Named contexts are supported since Dockerfile v1.4. Use #syntax directive in Dockerfile or update to latest BuildKit."
			return &wrapped{err, msg}
		}
	}
	return err
}

type wrapped struct {
	err error
	msg string
}

func (w *wrapped) Error() string {
	return w.msg
}

func (w *wrapped) Unwrap() error {
	return w.err
}

func isExperimental() bool {
	if v, ok := os.LookupEnv("BUILDX_EXPERIMENTAL"); ok {
		vv, _ := strconv.ParseBool(v)
		return vv
	}
	return false
}

func updateLastActivity(dockerCli command.Cli, ng *store.NodeGroup) error {
	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()
	return txn.UpdateLastActivity(ng)
}

func launchControllerAndRunBuild(dockerCli command.Cli, options buildOptions) error {
	ctx := context.TODO()

	if options.Quiet && options.progress != "auto" && options.progress != "quiet" {
		return errors.Errorf("progress=%s and quiet cannot be used together", options.progress)
	} else if options.Quiet {
		options.progress = "quiet"
	}
	if options.invoke != "" && (options.DockerfileName == "-" || options.ContextPath == "-") {
		// stdin must be usable for monitor
		return errors.Errorf("Dockerfile or context from stdin is not supported with invoke")
	}
	var invokeConfig controllerapi.ContainerConfig
	if inv := options.invoke; inv != "" {
		var err error
		invokeConfig, err = parseInvokeConfig(inv) // TODO: produce *controller.ContainerConfig directly.
		if err != nil {
			return err
		}
	}

	c, err := controller.NewController(ctx, options.ControlOptions, dockerCli)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil {
			logrus.Warnf("failed to close server connection %v", err)
		}
	}()

	f := ioset.NewSingleForwarder()
	pr, pw := io.Pipe()
	f.SetWriter(pw, func() io.WriteCloser {
		pw.Close() // propagate EOF
		logrus.Debug("propagating stdin close")
		return nil
	})
	f.SetReader(os.Stdin)

	// Start build
	ref, err := c.Build(ctx, options.BuildOptions, pr, os.Stdout, os.Stderr, options.progress)
	if err != nil {
		return errors.Wrapf(err, "failed to build") // TODO: allow invoke even on error
	}
	if err := pw.Close(); err != nil {
		logrus.Debug("failed to close stdin pipe writer")
	}
	if err := pr.Close(); err != nil {
		logrus.Debug("failed to close stdin pipe reader")
	}

	// post-build operations
	if options.invoke != "" {
		pr2, pw2 := io.Pipe()
		f.SetWriter(pw2, func() io.WriteCloser {
			pw2.Close() // propagate EOF
			return nil
		})
		con := console.Current()
		if err := con.SetRaw(); err != nil {
			if err := c.Disconnect(ctx, ref); err != nil {
				logrus.Warnf("disconnect error: %v", err)
			}
			return errors.Errorf("failed to configure terminal: %v", err)
		}
		err = monitor.RunMonitor(ctx, ref, options.BuildOptions, invokeConfig, c, options.progress, pr2, os.Stdout, os.Stderr)
		con.Reset()
		if err := pw2.Close(); err != nil {
			logrus.Debug("failed to close monitor stdin pipe reader")
		}
		if err != nil {
			logrus.Warnf("failed to run monitor: %v", err)
		}
	} else {
		if err := c.Disconnect(ctx, ref); err != nil {
			logrus.Warnf("disconnect error: %v", err)
		}
		// If "invoke" isn't specified, further inspection ins't provided. Finish the buildx server.
		if err := c.Kill(ctx); err != nil {
			return err
		}
	}
	return nil
}

func parseInvokeConfig(invoke string) (cfg controllerapi.ContainerConfig, err error) {
	cfg.Tty = true
	if invoke == "default" {
		return cfg, nil
	}

	csvReader := csv.NewReader(strings.NewReader(invoke))
	fields, err := csvReader.Read()
	if err != nil {
		return cfg, err
	}
	if len(fields) == 1 && !strings.Contains(fields[0], "=") {
		cfg.Cmd = []string{fields[0]}
		return cfg, nil
	}
	cfg.NoUser = true
	cfg.NoCwd = true
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return cfg, errors.Errorf("invalid value %s", field)
		}
		key := strings.ToLower(parts[0])
		value := parts[1]
		switch key {
		case "args":
			cfg.Cmd = append(cfg.Cmd, value) // TODO: support JSON
		case "entrypoint":
			cfg.Entrypoint = append(cfg.Entrypoint, value) // TODO: support JSON
		case "env":
			cfg.Env = append(cfg.Env, value)
		case "user":
			cfg.User = value
			cfg.NoUser = false
		case "cwd":
			cfg.Cwd = value
			cfg.NoCwd = false
		case "tty":
			cfg.Tty, err = strconv.ParseBool(value)
			if err != nil {
				return cfg, errors.Errorf("failed to parse tty: %v", err)
			}
		default:
			return cfg, errors.Errorf("unknown key %q", key)
		}
	}
	return cfg, nil
}

func newBuildOptions() buildOptions {
	return buildOptions{
		BuildOptions: controllerapi.BuildOptions{
			Opts: &controllerapi.CommonOptions{},
		},
	}
}

func newUlimits(opt *buildOptions) *ulimits {
	ul := make(map[string]*units.Ulimit)
	return &ulimits{opt: opt, org: dockeropts.NewUlimitOpt(&ul)}
}

type ulimits struct {
	opt *buildOptions
	org *dockeropts.UlimitOpt
}

func (u *ulimits) sync() {
	du := &controllerapi.UlimitOpt{
		Values: make(map[string]*controllerapi.Ulimit),
	}
	for _, l := range u.org.GetList() {
		du.Values[l.Name] = &controllerapi.Ulimit{
			Name: l.Name,
			Hard: l.Hard,
			Soft: l.Soft,
		}
	}
	u.opt.Ulimits = du
}

func (u *ulimits) String() string {
	return u.org.String()
}

func (u *ulimits) Set(v string) error {
	err := u.org.Set(v)
	u.sync()
	return err
}

func (u *ulimits) Type() string {
	return u.org.Type()
}

func newShmSize(opt *buildOptions) *shmSize {
	return &shmSize{opt: opt}
}

type shmSize struct {
	opt *buildOptions
	org dockeropts.MemBytes
}

func (s *shmSize) sync() {
	s.opt.ShmSize = s.org.Value()
}

func (s *shmSize) String() string {
	return s.org.String()
}

func (s *shmSize) Set(v string) error {
	err := s.org.Set(v)
	s.sync()
	return err
}

func (s *shmSize) Type() string {
	return s.org.Type()
}
