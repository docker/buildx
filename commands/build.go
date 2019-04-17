package commands

import (
	"context"
	"os"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tonistiigi/buildx/build"
	"github.com/tonistiigi/buildx/util/progress"
)

type buildOptions struct {
	commonOptions
	contextPath    string
	dockerfileName string
	tags           []string
	labels         []string
	buildArgs      []string

	cacheFrom   []string
	target      string
	platforms   []string
	secrets     []string
	ssh         []string
	outputs     []string
	imageIDFile string
	extraHosts  []string
	networkMode string

	// unimplemented
	squash bool
	quiet  bool

	// hidden
	// untrusted   bool
	// ulimits        *opts.UlimitOpt
	// memory         opts.MemBytes
	// memorySwap     opts.MemSwapBytes
	// shmSize        opts.MemBytes
	// cpuShares      int64
	// cpuPeriod      int64
	// cpuQuota       int64
	// cpuSetCpus     string
	// cpuSetMems     string
	// cgroupParent   string
	// isolation      string
	// compress    bool
	// securityOpt []string
}

type commonOptions struct {
	noCache  bool
	progress string
	pull     bool
}

func runBuild(dockerCli command.Cli, in buildOptions) error {
	if in.squash {
		return errors.Errorf("squash currently not implemented")
	}
	if in.quiet {
		return errors.Errorf("quiet currently not implemented")
	}

	ctx := appcontext.Context()

	opts := build.Options{
		Inputs: build.Inputs{
			ContextPath:    in.contextPath,
			DockerfilePath: in.dockerfileName,
			InStream:       os.Stdin,
		},
		Tags:        in.tags,
		Labels:      listToMap(in.labels),
		BuildArgs:   listToMap(in.buildArgs),
		Pull:        in.pull,
		NoCache:     in.noCache,
		Target:      in.target,
		ImageIDFile: in.imageIDFile,
		ExtraHosts:  in.extraHosts,
		NetworkMode: in.networkMode,
	}

	platforms, err := build.ParsePlatformSpecs(in.platforms)
	if err != nil {
		return err
	}
	opts.Platforms = platforms

	opts.Session = append(opts.Session, authprovider.NewDockerAuthProvider())

	secrets, err := build.ParseSecretSpecs(in.secrets)
	if err != nil {
		return err
	}
	opts.Session = append(opts.Session, secrets)

	ssh, err := build.ParseSSHSpecs(in.ssh)
	if err != nil {
		return err
	}
	opts.Session = append(opts.Session, ssh)

	outputs, err := build.ParseOutputs(in.outputs)
	if err != nil {
		return err
	}
	opts.Exports = outputs

	return buildTargets(ctx, dockerCli, map[string]build.Options{"default": opts}, in.progress)
}

func buildTargets(ctx context.Context, dockerCli command.Cli, opts map[string]build.Options, progressMode string) error {
	dis, err := getDefaultDrivers(ctx, dockerCli)
	if err != nil {
		return err
	}

	ctx2, cancel := context.WithCancel(context.TODO())
	defer cancel()
	pw := progress.NewPrinter(ctx2, os.Stderr, progressMode)

	_, err = build.Build(ctx, dis, opts, dockerAPI(dockerCli), pw)
	return err
}

func buildCmd(dockerCli command.Cli) *cobra.Command {
	var options buildOptions

	cmd := &cobra.Command{
		Use:     "build [OPTIONS] PATH | URL | -",
		Aliases: []string{"b"},
		Short:   "Start a build",
		Args:    cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.contextPath = args[0]
			return runBuild(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	flags.StringArrayVarP(&options.tags, "tag", "t", []string{}, "Name and optionally a tag in the 'name:tag' format")
	flags.StringArrayVar(&options.buildArgs, "build-arg", []string{}, "Set build-time variables")
	flags.StringVarP(&options.dockerfileName, "file", "f", "", "Name of the Dockerfile (Default is 'PATH/Dockerfile')")

	flags.StringArrayVar(&options.labels, "label", []string{}, "Set metadata for an image")

	flags.StringSliceVar(&options.cacheFrom, "cache-from", []string{}, "Images to consider as cache sources")

	flags.StringVar(&options.target, "target", "", "Set the target build stage to build.")

	// not implemented
	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Suppress the build output and print image ID on success")
	flags.StringVar(&options.networkMode, "network", "default", "Set the networking mode for the RUN instructions during build")
	flags.StringSliceVar(&options.extraHosts, "add-host", []string{}, "Add a custom host-to-IP mapping (host:ip)")
	flags.StringVar(&options.imageIDFile, "iidfile", "", "Write the image ID to the file")
	flags.BoolVar(&options.squash, "squash", false, "Squash newly built layers into a single new layer")
	flags.MarkHidden("quiet")
	flags.MarkHidden("squash")

	// hidden flags
	var ignore string
	var ignoreSlice []string
	var ignoreBool bool
	var ignoreInt int64
	flags.StringVar(&ignore, "ulimit", "", "Ulimit options")
	flags.MarkHidden("ulimit")
	flags.StringSliceVar(&ignoreSlice, "security-opt", []string{}, "Security options")
	flags.MarkHidden("security-opt")
	flags.BoolVar(&ignoreBool, "compress", false, "Compress the build context using gzip")
	flags.MarkHidden("compress")
	flags.StringVarP(&ignore, "memory", "m", "", "Memory limit")
	flags.MarkHidden("memory")
	flags.StringVar(&ignore, "memory-swap", "", "Swap limit equal to memory plus swap: '-1' to enable unlimited swap")
	flags.MarkHidden("memory-swap")
	flags.StringVar(&ignore, "shm-size", "", "Size of /dev/shm")
	flags.MarkHidden("shm-size")
	flags.Int64VarP(&ignoreInt, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	flags.MarkHidden("cpu-shares")
	flags.Int64Var(&ignoreInt, "cpu-period", 0, "Limit the CPU CFS (Completely Fair Scheduler) period")
	flags.MarkHidden("cpu-period")
	flags.Int64Var(&ignoreInt, "cpu-quota", 0, "Limit the CPU CFS (Completely Fair Scheduler) quota")
	flags.MarkHidden("cpu-quota")
	flags.StringVar(&ignore, "cpuset-cpus", "", "CPUs in which to allow execution (0-3, 0,1)")
	flags.MarkHidden("cpuset-cpus")
	flags.StringVar(&ignore, "cpuset-mems", "", "MEMs in which to allow execution (0-3, 0,1)")
	flags.MarkHidden("cpuset-mems")
	flags.StringVar(&ignore, "cgroup-parent", "", "Optional parent cgroup for the container")
	flags.MarkHidden("cgroup-parent")
	flags.StringVar(&ignore, "isolation", "", "Container isolation technology")
	flags.MarkHidden("isolation")

	platformsDefault := []string{}
	if v := os.Getenv("DOCKER_DEFAULT_PLATFORM"); v != "" {
		platformsDefault = []string{v}
	}
	flags.StringArrayVar(&options.platforms, "platform", platformsDefault, "Set target platform for build")

	flags.StringArrayVar(&options.secrets, "secret", []string{}, "Secret file to expose to the build: id=mysecret,src=/local/secret")

	flags.StringArrayVar(&options.ssh, "ssh", []string{}, "SSH agent socket or keys to expose to the build (format: default|<id>[=<socket>|<key>[,<key>]])")

	flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, "Output destination (format: type=local,dest=path)")

	commonFlags(&options.commonOptions, flags)

	return cmd
}

func commonFlags(options *commonOptions, flags *pflag.FlagSet) {
	flags.BoolVar(&options.noCache, "no-cache", false, "Do not use cache when building the image")
	flags.StringVar(&options.progress, "progress", "auto", "Set type of progress output (auto, plain, tty). Use plain to show container output")
	flags.BoolVar(&options.pull, "pull", false, "Always attempt to pull a newer version of the image")
}

func listToMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		kv := strings.SplitN(value, "=", 2)
		if len(kv) == 1 {
			result[kv[0]] = ""
		} else {
			result[kv[0]] = kv[1]
		}
	}
	return result
}
