package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	dockeropts "github.com/docker/cli/opts"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const defaultTargetName = "default"

type buildOptions struct {
	contextPath    string
	dockerfileName string

	allow       []string
	buildArgs   []string
	cacheFrom   []string
	cacheTo     []string
	extraHosts  []string
	imageIDFile string
	labels      []string
	networkMode string
	outputs     []string
	platforms   []string
	quiet       bool
	secrets     []string
	shmSize     dockeropts.MemBytes
	ssh         []string
	tags        []string
	target      string
	ulimits     *dockeropts.UlimitOpt
	commonOptions

	// unimplemented
	squash bool
}

type commonOptions struct {
	builder      string
	metadataFile string
	noCache      *bool
	progress     string
	pull         *bool

	// golangci-lint#826
	// nolint:structcheck
	exportPush bool
	// nolint:structcheck
	exportLoad bool
}

func runBuild(dockerCli command.Cli, in buildOptions) (err error) {
	if in.squash {
		return errors.Errorf("squash currently not implemented")
	}
	ctx := appcontext.Context()

	ctx, end, err := tracing.TraceCurrentCommand(ctx, "build")
	if err != nil {
		return err
	}
	defer func() {
		end(err)
	}()

	noCache := false
	if in.noCache != nil {
		noCache = *in.noCache
	}
	pull := false
	if in.pull != nil {
		pull = *in.pull
	}

	if in.quiet && in.progress != "auto" && in.progress != "quiet" {
		return errors.Errorf("progress=%s and quiet cannot be used together", in.progress)
	} else if in.quiet {
		in.progress = "quiet"
	}

	opts := build.Options{
		Inputs: build.Inputs{
			ContextPath:    in.contextPath,
			DockerfilePath: in.dockerfileName,
			InStream:       os.Stdin,
		},
		BuildArgs:   listToMap(in.buildArgs, true),
		ExtraHosts:  in.extraHosts,
		ImageIDFile: in.imageIDFile,
		Labels:      listToMap(in.labels, false),
		NetworkMode: in.networkMode,
		NoCache:     noCache,
		Pull:        pull,
		ShmSize:     in.shmSize,
		Tags:        in.tags,
		Target:      in.target,
		Ulimits:     in.ulimits,
	}

	platforms, err := platformutil.Parse(in.platforms)
	if err != nil {
		return err
	}
	opts.Platforms = platforms

	opts.Session = append(opts.Session, authprovider.NewDockerAuthProvider(os.Stderr))

	secrets, err := buildflags.ParseSecretSpecs(in.secrets)
	if err != nil {
		return err
	}
	opts.Session = append(opts.Session, secrets)

	sshSpecs := in.ssh
	if len(sshSpecs) == 0 && buildflags.IsGitSSH(in.contextPath) {
		sshSpecs = []string{"default"}
	}
	ssh, err := buildflags.ParseSSHSpecs(sshSpecs)
	if err != nil {
		return err
	}
	opts.Session = append(opts.Session, ssh)

	outputs, err := buildflags.ParseOutputs(in.outputs)
	if err != nil {
		return err
	}
	if in.exportPush {
		if in.exportLoad {
			return errors.Errorf("push and load may not be set together at the moment")
		}
		if len(outputs) == 0 {
			outputs = []client.ExportEntry{{
				Type: "image",
				Attrs: map[string]string{
					"push": "true",
				},
			}}
		} else {
			switch outputs[0].Type {
			case "image":
				outputs[0].Attrs["push"] = "true"
			default:
				return errors.Errorf("push and %q output can't be used together", outputs[0].Type)
			}
		}
	}
	if in.exportLoad {
		if len(outputs) == 0 {
			outputs = []client.ExportEntry{{
				Type:  "docker",
				Attrs: map[string]string{},
			}}
		} else {
			switch outputs[0].Type {
			case "docker":
			default:
				return errors.Errorf("load and %q output can't be used together", outputs[0].Type)
			}
		}
	}

	opts.Exports = outputs

	cacheImports, err := buildflags.ParseCacheEntry(in.cacheFrom)
	if err != nil {
		return err
	}
	opts.CacheFrom = cacheImports

	cacheExports, err := buildflags.ParseCacheEntry(in.cacheTo)
	if err != nil {
		return err
	}
	opts.CacheTo = cacheExports

	allow, err := buildflags.ParseEntitlements(in.allow)
	if err != nil {
		return err
	}
	opts.Allow = allow

	// key string used for kubernetes "sticky" mode
	contextPathHash, err := filepath.Abs(in.contextPath)
	if err != nil {
		contextPathHash = in.contextPath
	}

	imageID, err := buildTargets(ctx, dockerCli, map[string]build.Options{defaultTargetName: opts}, in.progress, contextPathHash, in.builder, in.metadataFile)
	if err != nil {
		return err
	}

	if in.quiet {
		fmt.Println(imageID)
	}
	return nil
}

func buildTargets(ctx context.Context, dockerCli command.Cli, opts map[string]build.Options, progressMode, contextPathHash, instance string, metadataFile string) (imageID string, err error) {
	dis, err := getInstanceOrDefault(ctx, dockerCli, instance, contextPathHash)
	if err != nil {
		return "", err
	}

	ctx2, cancel := context.WithCancel(context.TODO())
	defer cancel()

	printer := progress.NewPrinter(ctx2, os.Stderr, progressMode)

	resp, err := build.Build(ctx, dis, opts, dockerAPI(dockerCli), dockerCli.ConfigFile(), printer)
	err1 := printer.Wait()
	if err == nil {
		err = err1
	}
	if err != nil {
		return "", err
	}

	if len(metadataFile) > 0 && resp != nil {
		mdatab, err := json.MarshalIndent(resp[defaultTargetName].ExporterResponse, "", "  ")
		if err != nil {
			return "", err
		}
		if err := ioutils.AtomicWriteFile(metadataFile, mdatab, 0644); err != nil {
			return "", err
		}
	}

	return resp[defaultTargetName].ExporterResponse["containerimage.digest"], err
}

func newBuildOptions() buildOptions {
	ulimits := make(map[string]*units.Ulimit)
	return buildOptions{
		ulimits: dockeropts.NewUlimitOpt(&ulimits),
	}
}

func buildCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	options := newBuildOptions()

	cmd := &cobra.Command{
		Use:     "build [OPTIONS] PATH | URL | -",
		Aliases: []string{"b"},
		Short:   "Start a build",
		Args:    cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.contextPath = args[0]
			options.builder = rootOpts.builder
			return runBuild(dockerCli, options)
		},
	}

	var platformsDefault []string
	if v := os.Getenv("DOCKER_DEFAULT_PLATFORM"); v != "" {
		platformsDefault = []string{v}
	}

	flags := cmd.Flags()

	flags.StringSliceVar(&options.extraHosts, "add-host", []string{}, "Add a custom host-to-IP mapping (format: `host:ip`)")
	flags.SetAnnotation("add-host", "docs.external.url", []string{"https://docs.docker.com/engine/reference/commandline/build/#add-entries-to-container-hosts-file---add-host"})

	flags.StringSliceVar(&options.allow, "allow", []string{}, "Allow extra privileged entitlement (e.g., `network.host`, `security.insecure`)")

	flags.StringArrayVar(&options.buildArgs, "build-arg", []string{}, "Set build-time variables")
	flags.SetAnnotation("build-arg", "docs.external.url", []string{"https://docs.docker.com/engine/reference/commandline/build/#set-build-time-variables---build-arg"})

	flags.StringArrayVar(&options.cacheFrom, "cache-from", []string{}, "External cache sources (e.g., `user/app:cache`, `type=local,src=path/to/dir`)")

	flags.StringArrayVar(&options.cacheTo, "cache-to", []string{}, "Cache export destinations (e.g., `user/app:cache`, `type=local,dest=path/to/dir`)")

	flags.StringVarP(&options.dockerfileName, "file", "f", "", "Name of the Dockerfile (default: `PATH/Dockerfile`)")
	flags.SetAnnotation("file", "docs.external.url", []string{"https://docs.docker.com/engine/reference/commandline/build/#specify-a-dockerfile--f"})

	flags.StringVar(&options.imageIDFile, "iidfile", "", "Write the image ID to the file")

	flags.StringArrayVar(&options.labels, "label", []string{}, "Set metadata for an image")

	flags.BoolVar(&options.exportLoad, "load", false, "Shorthand for `--output=type=docker`")

	flags.StringVar(&options.networkMode, "network", "default", "Set the networking mode for the RUN instructions during build")

	flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, "Output destination (format: `type=local,dest=path`)")

	flags.StringArrayVar(&options.platforms, "platform", platformsDefault, "Set target platform for build")

	flags.BoolVar(&options.exportPush, "push", false, "Shorthand for `--output=type=registry`")

	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Suppress the build output and print image ID on success")

	flags.StringArrayVar(&options.secrets, "secret", []string{}, "Secret file to expose to the build (format: `id=mysecret,src=/local/secret`)")

	flags.Var(&options.shmSize, "shm-size", "Size of `/dev/shm`")

	flags.StringArrayVar(&options.ssh, "ssh", []string{}, "SSH agent socket or keys to expose to the build (format: `default|<id>[=<socket>|<key>[,<key>]]`)")

	flags.StringArrayVarP(&options.tags, "tag", "t", []string{}, "Name and optionally a tag (format: `name:tag`)")
	flags.SetAnnotation("tag", "docs.external.url", []string{"https://docs.docker.com/engine/reference/commandline/build/#tag-an-image--t"})

	flags.StringVar(&options.target, "target", "", "Set the target build stage to build.")
	flags.SetAnnotation("target", "docs.external.url", []string{"https://docs.docker.com/engine/reference/commandline/build/#specifying-target-build-stage---target"})

	flags.Var(options.ulimits, "ulimit", "Ulimit options")

	// not implemented
	flags.BoolVar(&options.squash, "squash", false, "Squash newly built layers into a single new layer")
	flags.MarkHidden("squash")

	// hidden flags
	var ignore string
	var ignoreSlice []string
	var ignoreBool bool
	var ignoreInt int64

	flags.StringVar(&ignore, "cgroup-parent", "", "Optional parent cgroup for the container")
	flags.MarkHidden("cgroup-parent")

	flags.BoolVar(&ignoreBool, "compress", false, "Compress the build context using gzip")
	flags.MarkHidden("compress")

	flags.StringVar(&ignore, "isolation", "", "Container isolation technology")
	flags.MarkHidden("isolation")

	flags.StringSliceVar(&ignoreSlice, "security-opt", []string{}, "Security options")
	flags.MarkHidden("security-opt")

	flags.StringVarP(&ignore, "memory", "m", "", "Memory limit")
	flags.MarkHidden("memory")

	flags.StringVar(&ignore, "memory-swap", "", "Swap limit equal to memory plus swap: `-1` to enable unlimited swap")
	flags.MarkHidden("memory-swap")

	flags.Int64VarP(&ignoreInt, "cpu-shares", "c", 0, "CPU shares (relative weight)")
	flags.MarkHidden("cpu-shares")

	flags.Int64Var(&ignoreInt, "cpu-period", 0, "Limit the CPU CFS (Completely Fair Scheduler) period")
	flags.MarkHidden("cpu-period")

	flags.Int64Var(&ignoreInt, "cpu-quota", 0, "Limit the CPU CFS (Completely Fair Scheduler) quota")
	flags.MarkHidden("cpu-quota")

	flags.StringVar(&ignore, "cpuset-cpus", "", "CPUs in which to allow execution (`0-3`, `0,1`)")
	flags.MarkHidden("cpuset-cpus")

	flags.StringVar(&ignore, "cpuset-mems", "", "MEMs in which to allow execution (`0-3`, `0,1`)")
	flags.MarkHidden("cpuset-mems")

	flags.BoolVar(&ignoreBool, "rm", true, "Remove intermediate containers after a successful build")
	flags.MarkHidden("rm")

	flags.BoolVar(&ignoreBool, "force-rm", false, "Always remove intermediate containers")
	flags.MarkHidden("force-rm")

	commonBuildFlags(&options.commonOptions, flags)
	return cmd
}

func commonBuildFlags(options *commonOptions, flags *pflag.FlagSet) {
	options.noCache = flags.Bool("no-cache", false, "Do not use cache when building the image")
	flags.StringVar(&options.progress, "progress", "auto", "Set type of progress output (`auto`, `plain`, `tty`). Use plain to show container output")
	options.pull = flags.Bool("pull", false, "Always attempt to pull a newer version of the image")
	flags.StringVar(&options.metadataFile, "metadata-file", "", "Write build result metadata to the file")
}

func listToMap(values []string, defaultEnv bool) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		kv := strings.SplitN(value, "=", 2)
		if len(kv) == 1 {
			if defaultEnv {
				v, ok := os.LookupEnv(kv[0])
				if ok {
					result[kv[0]] = v
				}
			} else {
				result[kv[0]] = ""
			}
		} else {
			result[kv[0]] = kv[1]
		}
	}
	return result
}
