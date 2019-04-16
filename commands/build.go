package commands

import (
	"context"
	"os"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/appcontext"
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
	// extraHosts     opts.ListOpts
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
	// quiet          bool
	cacheFrom []string
	// compress    bool
	// securityOpt []string
	// networkMode string
	// squash      bool
	target string
	// imageIDFile string
	platforms []string
	// untrusted   bool
	secrets []string
	ssh     []string
	outputs []string
}

type commonOptions struct {
	noCache  bool
	progress string
	pull     bool
}

func runBuild(dockerCli command.Cli, in buildOptions) error {
	ctx := appcontext.Context()

	opts := build.Options{
		Inputs: build.Inputs{
			ContextPath:    in.contextPath,
			DockerfilePath: in.dockerfileName,
			InStream:       os.Stdin,
		},
		Tags:      in.tags,
		Labels:    listToMap(in.labels),
		BuildArgs: listToMap(in.buildArgs),
		Pull:      in.pull,
		NoCache:   in.noCache,
		Target:    in.target,
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

	_, err = build.Build(ctx, dis, opts, pw)
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
	// flags.Var(options.ulimits, "ulimit", "Ulimit options")
	flags.StringVarP(&options.dockerfileName, "file", "f", "", "Name of the Dockerfile (Default is 'PATH/Dockerfile')")
	// flags.VarP(&options.memory, "memory", "m", "Memory limit")
	// flags.Var(&options.memorySwap, "memory-swap", "Swap limit equal to memory plus swap: '-1' to enable unlimited swap")
	// flags.Var(&options.shmSize, "shm-size", "Size of /dev/shm")
	// flags.Int64VarP(&options.cpuShares, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	// flags.Int64Var(&options.cpuPeriod, "cpu-period", 0, "Limit the CPU CFS (Completely Fair Scheduler) period")
	// flags.Int64Var(&options.cpuQuota, "cpu-quota", 0, "Limit the CPU CFS (Completely Fair Scheduler) quota")
	// flags.StringVar(&options.cpuSetCpus, "cpuset-cpus", "", "CPUs in which to allow execution (0-3, 0,1)")
	// flags.StringVar(&options.cpuSetMems, "cpuset-mems", "", "MEMs in which to allow execution (0-3, 0,1)")
	// flags.StringVar(&options.cgroupParent, "cgroup-parent", "", "Optional parent cgroup for the container")
	// flags.StringVar(&options.isolation, "isolation", "", "Container isolation technology")
	flags.StringArrayVar(&options.labels, "label", []string{}, "Set metadata for an image")
	// flags.BoolVarP(&options.quiet, "quiet", "q", false, "Suppress the build output and print image ID on success")
	flags.StringSliceVar(&options.cacheFrom, "cache-from", []string{}, "Images to consider as cache sources")
	// flags.BoolVar(&options.compress, "compress", false, "Compress the build context using gzip")

	// flags.StringSliceVar(&options.securityOpt, "security-opt", []string{}, "Security options")
	// flags.StringVar(&options.networkMode, "network", "default", "Set the networking mode for the RUN instructions during build")
	// flags.Var(&options.extraHosts, "add-host", "Add a custom host-to-IP mapping (host:ip)")
	flags.StringVar(&options.target, "target", "", "Set the target build stage to build.")
	// flags.StringVar(&options.imageIDFile, "iidfile", "", "Write the image ID to the file")

	platformsDefault := []string{}
	if v := os.Getenv("DOCKER_DEFAULT_PLATFORM"); v != "" {
		platformsDefault = []string{v}
	}
	flags.StringArrayVar(&options.platforms, "platform", platformsDefault, "Set target platform for build")

	// flags.BoolVar(&options.squash, "squash", false, "Squash newly built layers into a single new layer")
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
