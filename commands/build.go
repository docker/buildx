package commands

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/console"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/monitor"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/buildx/util/metricutil"
	"github.com/docker/buildx/util/osutil"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/buildx/util/tracing"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	dockeropts "github.com/docker/cli/opts"
	"github.com/docker/docker/api/types/versions"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/frontend/subrequests"
	"github.com/moby/buildkit/frontend/subrequests/lint"
	"github.com/moby/buildkit/frontend/subrequests/outline"
	"github.com/moby/buildkit/frontend/subrequests/targets"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/solver/errdefs"
	solverpb "github.com/moby/buildkit/solver/pb"
	sourcepolicy "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/moby/sys/atomicwriter"
	"github.com/morikuni/aec"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/tonistiigi/go-csvvalue"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

type buildOptions struct {
	allow          []string
	annotations    []string
	buildArgs      []string
	cacheFrom      []string
	cacheTo        []string
	cgroupParent   string
	contextPath    string
	contexts       []string
	dockerfileName string
	extraHosts     []string
	imageIDFile    string
	labels         []string
	networkMode    string
	noCacheFilter  []string
	outputs        []string
	platforms      []string
	callFunc       string
	secrets        []string
	shmSize        dockeropts.MemBytes
	ssh            []string
	tags           []string
	target         string
	ulimits        *dockeropts.UlimitOpt

	attests    []string
	sbom       string
	provenance string

	progress string
	quiet    bool

	builder      string
	metadataFile string
	noCache      bool
	pull         bool
	exportPush   bool
	exportLoad   bool

	invokeConfig *invokeConfig
}

func (o *buildOptions) toOptions() (*BuildOptions, error) {
	var err error

	buildArgs, err := listToMap(o.buildArgs, true)
	if err != nil {
		return nil, err
	}

	labels, err := listToMap(o.labels, false)
	if err != nil {
		return nil, err
	}

	opts := BuildOptions{
		Allow:          o.allow,
		Annotations:    o.annotations,
		BuildArgs:      buildArgs,
		CgroupParent:   o.cgroupParent,
		ContextPath:    o.contextPath,
		DockerfileName: o.dockerfileName,
		ExtraHosts:     o.extraHosts,
		Labels:         labels,
		NetworkMode:    o.networkMode,
		NoCacheFilter:  o.noCacheFilter,
		Platforms:      o.platforms,
		ShmSize:        int64(o.shmSize),
		Tags:           o.tags,
		Target:         o.target,
		Ulimits:        o.ulimits,
		Builder:        o.builder,
		NoCache:        o.noCache,
		Pull:           o.pull,
		ExportPush:     o.exportPush,
		ExportLoad:     o.exportLoad,
	}

	// TODO: extract env var parsing to a method easily usable by library consumers
	if v := os.Getenv("SOURCE_DATE_EPOCH"); v != "" {
		if _, ok := opts.BuildArgs["SOURCE_DATE_EPOCH"]; !ok {
			opts.BuildArgs["SOURCE_DATE_EPOCH"] = v
		}
	}

	opts.SourcePolicy, err = build.ReadSourcePolicy()
	if err != nil {
		return nil, err
	}

	inAttests := slices.Clone(o.attests)
	if o.provenance != "" {
		inAttests = append(inAttests, buildflags.CanonicalizeAttest("provenance", o.provenance))
	}
	if o.sbom != "" {
		inAttests = append(inAttests, buildflags.CanonicalizeAttest("sbom", o.sbom))
	}
	opts.Attests, err = buildflags.ParseAttests(inAttests)
	if err != nil {
		return nil, err
	}

	opts.NamedContexts, err = buildflags.ParseContextNames(o.contexts)
	if err != nil {
		return nil, err
	}

	opts.Exports, err = buildflags.ParseExports(o.outputs)
	if err != nil {
		return nil, err
	}
	for _, e := range opts.Exports {
		if (e.Type == client.ExporterLocal || e.Type == client.ExporterTar) && o.imageIDFile != "" {
			return nil, errors.Errorf("local and tar exporters are incompatible with image ID file")
		}
	}

	opts.CacheFrom, err = buildflags.ParseCacheEntry(o.cacheFrom)
	if err != nil {
		return nil, err
	}

	opts.CacheTo, err = buildflags.ParseCacheEntry(o.cacheTo)
	if err != nil {
		return nil, err
	}

	opts.Secrets, err = buildflags.ParseSecretSpecs(o.secrets)
	if err != nil {
		return nil, err
	}
	opts.SSH, err = buildflags.ParseSSHSpecs(o.ssh)
	if err != nil {
		return nil, err
	}

	opts.CallFunc, err = buildflags.ParseCallFunc(o.callFunc)
	if err != nil {
		return nil, err
	}

	prm := confutil.MetadataProvenance()
	if opts.CallFunc != nil || len(o.metadataFile) == 0 {
		prm = confutil.MetadataProvenanceModeDisabled
	}
	opts.ProvenanceResponseMode = string(prm)

	return &opts, nil
}

func (o *buildOptions) toDisplayMode() (progressui.DisplayMode, error) {
	progress := progressui.DisplayMode(o.progress)
	if o.quiet {
		if progress != progressui.AutoMode && progress != progressui.QuietMode {
			return "", errors.Errorf("progress=%s and quiet cannot be used together", o.progress)
		}
		return progressui.QuietMode, nil
	}
	return progress, nil
}

const (
	commandNameAttribute = attribute.Key("command.name")
	commandOptionsHash   = attribute.Key("command.options.hash")
	driverNameAttribute  = attribute.Key("driver.name")
	driverTypeAttribute  = attribute.Key("driver.type")
)

func buildMetricAttributes(dockerCli command.Cli, driverType string, options *buildOptions) attribute.Set {
	return attribute.NewSet(
		commandNameAttribute.String("build"),
		attribute.Stringer(string(commandOptionsHash), &buildOptionsHash{
			buildOptions: options,
			cfg:          confutil.NewConfig(dockerCli),
		}),
		driverNameAttribute.String(options.builder),
		driverTypeAttribute.String(driverType),
	)
}

// buildOptionsHash computes a hash for the buildOptions when the String method is invoked.
// This is done so we can delay the computation of the hash until needed by OTEL using
// the fmt.Stringer interface.
type buildOptionsHash struct {
	*buildOptions
	cfg        *confutil.Config
	result     string
	resultOnce sync.Once
}

func (o *buildOptionsHash) String() string {
	o.resultOnce.Do(func() {
		target := o.target
		contextPath := o.contextPath
		dockerfile := o.dockerfileName
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}

		if contextPath != "-" && osutil.IsLocalDir(contextPath) {
			contextPath = osutil.ToAbs(contextPath)
		}
		salt := o.cfg.TryNodeIdentifier()

		h := sha256.New()
		for _, s := range []string{target, contextPath, dockerfile, salt} {
			_, _ = io.WriteString(h, s)
			h.Write([]byte{0})
		}
		o.result = hex.EncodeToString(h.Sum(nil))
	})
	return o.result
}

func runBuild(ctx context.Context, dockerCli command.Cli, options buildOptions) (err error) {
	mp := dockerCli.MeterProvider()

	ctx, end, err := tracing.TraceCurrentCommand(ctx, []string{"build", options.contextPath},
		attribute.String("builder", options.builder),
		attribute.String("context", options.contextPath),
		attribute.String("dockerfile", options.dockerfileName),
	)
	if err != nil {
		return err
	}
	defer func() {
		end(err)
	}()

	opts, err := options.toOptions()
	if err != nil {
		return err
	}

	// Avoid leaving a stale file if we eventually fail
	if options.imageIDFile != "" {
		if err := os.Remove(options.imageIDFile); err != nil && !os.IsNotExist(err) {
			return errors.Wrap(err, "removing image ID file")
		}
	}

	contextPathHash := options.contextPath
	if absContextPath, err := filepath.Abs(contextPathHash); err == nil {
		contextPathHash = absContextPath
	}
	b, err := builder.New(dockerCli,
		builder.WithName(options.builder),
		builder.WithContextPathHash(contextPathHash),
	)
	if err != nil {
		return err
	}
	_, err = b.LoadNodes(ctx)
	if err != nil {
		return err
	}
	driverType := b.Driver

	var term bool
	if _, err := console.ConsoleFromFile(os.Stderr); err == nil {
		term = true
	}
	attributes := buildMetricAttributes(dockerCli, driverType, &options)

	ctx2, cancel := context.WithCancelCause(context.TODO())
	defer func() { cancel(errors.WithStack(context.Canceled)) }()
	progressMode, err := options.toDisplayMode()
	if err != nil {
		return err
	}
	var printer *progress.Printer
	printer, err = progress.NewPrinter(ctx2, os.Stderr, progressMode,
		progress.WithDesc(
			fmt.Sprintf("building with %q instance using %s driver", b.Name, b.Driver),
			fmt.Sprintf("%s:%s", b.Driver, b.Name),
		),
		progress.WithMetrics(mp, attributes),
		progress.WithOnClose(func() {
			printWarnings(os.Stderr, printer.Warnings(), progressMode)
		}),
	)
	if err != nil {
		return err
	}

	done := timeBuildCommand(mp, attributes)
	resp, inputs, retErr := runBuildWithOptions(ctx, dockerCli, opts, options, printer)

	if err := printer.Wait(); retErr == nil {
		retErr = err
	}

	done(retErr)
	if retErr != nil {
		return retErr
	}

	switch progressMode {
	case progressui.RawJSONMode:
		// no additional display
	case progressui.QuietMode:
		fmt.Println(getImageID(resp.ExporterResponse))
	default:
		desktop.PrintBuildDetails(os.Stderr, printer.BuildRefs(), term)
	}
	if options.imageIDFile != "" {
		if err := os.WriteFile(options.imageIDFile, []byte(getImageID(resp.ExporterResponse)), 0644); err != nil {
			return errors.Wrap(err, "writing image ID file")
		}
	}
	if options.metadataFile != "" {
		dt := decodeExporterResponse(resp.ExporterResponse)
		if opts.CallFunc == nil {
			if warnings := printer.Warnings(); len(warnings) > 0 && confutil.MetadataWarningsEnabled() {
				dt["buildx.build.warnings"] = warnings
			}
		}
		if err := writeMetadataFile(options.metadataFile, dt); err != nil {
			return err
		}
	}
	if opts.CallFunc != nil {
		if exitCode, err := printResult(dockerCli.Out(), opts.CallFunc, resp.ExporterResponse, options.target, inputs); err != nil {
			return err
		} else if exitCode != 0 {
			return cobrautil.ExitCodeError(exitCode)
		}
	}
	if v, ok := resp.ExporterResponse["frontend.result.inlinemessage"]; ok {
		fmt.Fprintf(dockerCli.Out(), "\n%s\n", v)
		return nil
	}
	return nil
}

// getImageID returns the image ID - the digest of the image config
func getImageID(resp map[string]string) string {
	dgst := resp[exptypes.ExporterImageDigestKey]
	if v, ok := resp[exptypes.ExporterImageConfigDigestKey]; ok {
		dgst = v
	}
	return dgst
}

func runBuildWithOptions(ctx context.Context, dockerCli command.Cli, opts *BuildOptions, options buildOptions, printer *progress.Printer) (_ *client.SolveResponse, _ *build.Inputs, retErr error) {
	if options.invokeConfig != nil && (options.dockerfileName == "-" || options.contextPath == "-") {
		// stdin must be usable for monitor
		return nil, nil, errors.Errorf("Dockerfile or context from stdin is not supported with invoke")
	}

	var (
		in io.ReadCloser
		m  *monitor.Monitor
		bh build.Handler
	)
	if options.invokeConfig == nil {
		in = dockerCli.In()
	} else {
		m = monitor.New(&options.invokeConfig.InvokeConfig, dockerCli.In(), os.Stdout, os.Stderr, printer)
		defer m.Close()

		bh = m.Handler()
	}

	for {
		resp, inputs, err := RunBuild(ctx, dockerCli, opts, in, printer, &bh)
		if err != nil {
			if errors.Is(err, build.ErrRestart) {
				retErr = nil
				continue
			}
			return nil, nil, errors.Wrapf(err, "failed to build")
		}

		return resp, inputs, err
	}
}

func buildCmd(dockerCli command.Cli, rootOpts *rootOptions, debugConfig *debugOptions) *cobra.Command {
	cFlags := &commonFlags{}
	options := &buildOptions{}

	cmd := &cobra.Command{
		Use:     "build [OPTIONS] PATH | URL | -",
		Short:   "Start a build",
		Args:    cli.ExactArgs(1),
		Aliases: []string{"b"},
		Annotations: map[string]string{
			"aliases": "docker build, docker builder build, docker image build, docker buildx b",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			options.contextPath = args[0]
			options.builder = rootOpts.builder
			options.metadataFile = cFlags.metadataFile
			options.noCache = false
			if cFlags.noCache != nil {
				options.noCache = *cFlags.noCache
			}
			options.pull = false
			if cFlags.pull != nil {
				options.pull = *cFlags.pull
			}
			options.progress = cFlags.progress
			cmd.Flags().VisitAll(checkWarnedFlags)

			if debugConfig != nil && (debugConfig.InvokeFlag != "" || debugConfig.OnFlag != "") {
				iConfig := new(invokeConfig)
				if err := iConfig.parseInvokeConfig(debugConfig.InvokeFlag, debugConfig.OnFlag); err != nil {
					return err
				}
				options.invokeConfig = iConfig
			}

			return runBuild(cmd.Context(), dockerCli, *options)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveFilterDirs
		},
	}

	var platformsDefault []string
	if v := os.Getenv("DOCKER_DEFAULT_PLATFORM"); v != "" {
		platformsDefault = []string{v}
	}

	flags := cmd.Flags()

	flags.StringSliceVar(&options.extraHosts, "add-host", []string{}, `Add a custom host-to-IP mapping (format: "host:ip")`)

	flags.StringArrayVar(&options.allow, "allow", []string{}, `Allow extra privileged entitlement (e.g., "network.host", "security.insecure")`)

	flags.StringArrayVarP(&options.annotations, "annotation", "", []string{}, "Add annotation to the image")

	flags.StringArrayVar(&options.buildArgs, "build-arg", []string{}, "Set build-time variables")

	flags.StringArrayVar(&options.cacheFrom, "cache-from", []string{}, `External cache sources (e.g., "user/app:cache", "type=local,src=path/to/dir")`)

	flags.StringArrayVar(&options.cacheTo, "cache-to", []string{}, `Cache export destinations (e.g., "user/app:cache", "type=local,dest=path/to/dir")`)

	flags.StringVar(&options.cgroupParent, "cgroup-parent", "", `Set the parent cgroup for the "RUN" instructions during build`)

	flags.StringArrayVar(&options.contexts, "build-context", []string{}, "Additional build contexts (e.g., name=path)")

	flags.StringVarP(&options.dockerfileName, "file", "f", "", `Name of the Dockerfile (default: "PATH/Dockerfile")`)

	flags.StringVar(&options.imageIDFile, "iidfile", "", "Write the image ID to a file")

	flags.StringArrayVar(&options.labels, "label", []string{}, "Set metadata for an image")

	flags.BoolVar(&options.exportLoad, "load", false, `Shorthand for "--output=type=docker"`)

	flags.StringVar(&options.networkMode, "network", "default", `Set the networking mode for the "RUN" instructions during build`)

	flags.StringArrayVar(&options.noCacheFilter, "no-cache-filter", []string{}, "Do not cache specified stages")

	flags.StringArrayVarP(&options.outputs, "output", "o", []string{}, `Output destination (format: "type=local,dest=path")`)

	flags.StringArrayVar(&options.platforms, "platform", platformsDefault, "Set target platform for build")

	flags.BoolVar(&options.exportPush, "push", false, `Shorthand for "--output=type=registry"`)

	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Suppress the build output and print image ID on success")

	flags.StringArrayVar(&options.secrets, "secret", []string{}, `Secret to expose to the build (format: "id=mysecret[,src=/local/secret]")`)

	flags.Var(&options.shmSize, "shm-size", `Shared memory size for build containers`)

	flags.StringArrayVar(&options.ssh, "ssh", []string{}, `SSH agent socket or keys to expose to the build (format: "default|<id>[=<socket>|<key>[,<key>]]")`)

	flags.StringArrayVarP(&options.tags, "tag", "t", []string{}, `Name and optionally a tag (format: "name:tag")`)

	flags.StringVar(&options.target, "target", "", "Set the target build stage to build")

	options.ulimits = dockeropts.NewUlimitOpt(nil)
	flags.Var(options.ulimits, "ulimit", "Ulimit options")

	flags.StringArrayVar(&options.attests, "attest", []string{}, `Attestation parameters (format: "type=sbom,generator=image")`)
	flags.StringVar(&options.sbom, "sbom", "", `Shorthand for "--attest=type=sbom"`)
	flags.StringVar(&options.provenance, "provenance", "", `Shorthand for "--attest=type=provenance"`)

	flags.StringVar(&options.callFunc, "call", "build", `Set method for evaluating build ("check", "outline", "targets")`)
	flags.VarPF(callAlias(&options.callFunc, "check"), "check", "", `Shorthand for "--call=check"`)
	flags.Lookup("check").NoOptDefVal = "true"

	// hidden flags
	var ignore string
	var ignoreSlice []string
	var ignoreBool bool
	var ignoreInt int64

	flags.StringVar(&options.callFunc, "print", "", "Print result of information request (e.g., outline, targets)")
	cobrautil.MarkFlagsExperimental(flags, "print")
	flags.MarkHidden("print")

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
	cobrautil.MarkFlagsExperimental(flags, "squash")

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
	flags.StringVar(&options.progress, "progress", "auto", `Set type of progress output ("auto", "quiet", "plain", "tty", "rawjson"). Use plain to show container output`)
	options.pull = flags.Bool("pull", false, "Always attempt to pull all referenced images")
	flags.StringVar(&options.metadataFile, "metadata-file", "", "Write build result metadata to a file")
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

func writeMetadataFile(filename string, dt any) error {
	b, err := json.MarshalIndent(dt, "", "  ")
	if err != nil {
		return err
	}
	return atomicwriter.WriteFile(filename, b, 0644)
}

func decodeExporterResponse(exporterResponse map[string]string) map[string]any {
	decFunc := func(k, v string) ([]byte, error) {
		if k == "result.json" {
			// result.json is part of metadata response for subrequests which
			// is already a JSON object: https://github.com/moby/buildkit/blob/f6eb72f2f5db07ddab89ac5e2bd3939a6444f4be/frontend/dockerui/requests.go#L100-L102
			return []byte(v), nil
		}
		return base64.StdEncoding.DecodeString(v)
	}
	out := make(map[string]any)
	for k, v := range exporterResponse {
		dt, err := decFunc(k, v)
		if err != nil {
			out[k] = v
			continue
		}
		var raw map[string]any
		if err = json.Unmarshal(dt, &raw); err != nil || len(raw) == 0 {
			var rawList []map[string]any
			if err = json.Unmarshal(dt, &rawList); err != nil || len(rawList) == 0 {
				out[k] = v
				continue
			}
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

func updateLastActivity(dockerCli command.Cli, ng *store.NodeGroup) error {
	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()
	return txn.UpdateLastActivity(ng)
}

func listToMap(values []string, defaultEnv bool) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		k, v, hasValue := strings.Cut(value, "=")
		if k == "" {
			return nil, errors.Errorf("invalid key-value pair %q: empty key", value)
		}
		if hasValue {
			result[k] = v
		} else if defaultEnv {
			if envVal, ok := os.LookupEnv(k); ok {
				result[k] = envVal
			}
		} else {
			result[k] = ""
		}
	}
	return result, nil
}

func printWarnings(w io.Writer, warnings []client.VertexWarning, mode progressui.DisplayMode) {
	if len(warnings) == 0 || mode == progressui.QuietMode || mode == progressui.RawJSONMode {
		return
	}
	fmt.Fprintf(w, "\n ")
	sb := &bytes.Buffer{}
	if len(warnings) == 1 {
		fmt.Fprintf(sb, "1 warning found")
	} else {
		fmt.Fprintf(sb, "%d warnings found", len(warnings))
	}
	if logrus.GetLevel() < logrus.DebugLevel {
		fmt.Fprintf(sb, " (use docker --debug to expand)")
	}
	fmt.Fprintf(sb, ":\n")
	fmt.Fprint(w, aec.Apply(sb.String(), aec.YellowF))

	for _, warn := range warnings {
		fmt.Fprintf(w, " - %s\n", warn.Short)
		if logrus.GetLevel() < logrus.DebugLevel {
			continue
		}
		for _, d := range warn.Detail {
			fmt.Fprintf(w, "%s\n", d)
		}
		if warn.URL != "" {
			fmt.Fprintf(w, "More info: %s\n", warn.URL)
		}
		if warn.SourceInfo != nil && warn.Range != nil {
			src := errdefs.Source{
				Info:   warn.SourceInfo,
				Ranges: warn.Range,
			}
			src.Print(w)
		}
		fmt.Fprintf(w, "\n")
	}
}

func printResult(w io.Writer, f *buildflags.CallFunc, res map[string]string, target string, inp *build.Inputs) (int, error) {
	switch f.Name {
	case "outline":
		return 0, printValue(w, outline.PrintOutline, outline.SubrequestsOutlineDefinition.Version, f.Format, res)
	case "targets":
		return 0, printValue(w, targets.PrintTargets, targets.SubrequestsTargetsDefinition.Version, f.Format, res)
	case "subrequests.describe":
		return 0, printValue(w, subrequests.PrintDescribe, subrequests.SubrequestsDescribeDefinition.Version, f.Format, res)
	case "lint":
		lintResults := lint.LintResults{}
		if result, ok := res["result.json"]; ok {
			if err := json.Unmarshal([]byte(result), &lintResults); err != nil {
				return 0, err
			}
		}

		warningCount := len(lintResults.Warnings)
		if f.Format != "json" && warningCount > 0 {
			var warningCountMsg string
			if warningCount == 1 {
				warningCountMsg = "1 warning has been found!"
			} else if warningCount > 1 {
				warningCountMsg = fmt.Sprintf("%d warnings have been found!", warningCount)
			}
			fmt.Fprintf(w, "Check complete, %s\n", warningCountMsg)
		}
		sourceInfoMap := func(sourceInfo *solverpb.SourceInfo) *solverpb.SourceInfo {
			if sourceInfo == nil || inp == nil {
				return sourceInfo
			}
			if target == "" {
				target = "default"
			}

			if inp.DockerfileMappingSrc != "" {
				newSourceInfo := proto.Clone(sourceInfo).(*solverpb.SourceInfo)
				newSourceInfo.Filename = inp.DockerfileMappingSrc
				return newSourceInfo
			}
			return sourceInfo
		}

		printLintWarnings := func(dt []byte, w io.Writer) error {
			return lintResults.PrintTo(w, sourceInfoMap)
		}

		err := printValue(w, printLintWarnings, lint.SubrequestLintDefinition.Version, f.Format, res)
		if err != nil {
			return 0, err
		}

		if lintResults.Error != nil {
			// Print the error message and the source
			// Normally, we would use `errdefs.WithSource` to attach the source to the
			// error and let the error be printed by the handling that's already in place,
			// but here we want to print the error in a way that's consistent with how
			// the lint warnings are printed via the `lint.PrintLintViolations` function,
			// which differs from the default error printing.
			if f.Format != "json" && len(lintResults.Warnings) > 0 {
				fmt.Fprintln(w)
			}
			lintBuf := bytes.NewBuffer(nil)
			lintResults.PrintErrorTo(lintBuf, sourceInfoMap)
			return 0, errors.New(lintBuf.String())
		} else if len(lintResults.Warnings) == 0 && f.Format != "json" {
			fmt.Fprintln(w, "Check complete, no warnings found.")
		}
	default:
		if dt, ok := res["result.json"]; ok && f.Format == "json" {
			fmt.Fprintln(w, dt)
		} else if dt, ok := res["result.txt"]; ok {
			fmt.Fprint(w, dt)
		} else {
			fmt.Fprintf(w, "%s %+v\n", f, res)
		}
	}
	if v, ok := res["result.statuscode"]; !f.IgnoreStatus && ok {
		if n, err := strconv.Atoi(v); err == nil && n != 0 {
			return n, nil
		}
	}
	return 0, nil
}

type callFunc func([]byte, io.Writer) error

func printValue(w io.Writer, printer callFunc, version string, format string, res map[string]string) error {
	if format == "json" {
		fmt.Fprintln(w, res["result.json"])
		return nil
	}

	if res["version"] != "" && versions.LessThan(version, res["version"]) && res["result.txt"] != "" {
		// structure is too new and we don't know how to print it
		fmt.Fprint(w, res["result.txt"])
		return nil
	}
	return printer([]byte(res["result.json"]), w)
}

type invokeConfig struct {
	build.InvokeConfig
	invokeFlag string
}

func (cfg *invokeConfig) parseInvokeConfig(invoke, on string) error {
	switch on {
	case "always":
		cfg.SuspendOn = build.SuspendAlways
	case "error":
		cfg.SuspendOn = build.SuspendError
	default:
		if invoke != "" {
			cfg.SuspendOn = build.SuspendAlways
		}
	}

	cfg.invokeFlag = invoke
	cfg.Tty = true
	cfg.NoCmd = true
	switch invoke {
	case "default", "":
		return nil
	case "on-error":
		// NOTE: we overwrite the command to run because the original one should fail on the failed step.
		// TODO: make this configurable via flags or restorable from LLB.
		// Discussion: https://github.com/docker/buildx/pull/1640#discussion_r1113295900
		cfg.Cmd = []string{"/bin/sh"}
		cfg.NoCmd = false
		return nil
	}

	csvParser := csvvalue.NewParser()
	csvParser.LazyQuotes = true
	fields, err := csvParser.Fields(invoke, nil)
	if err != nil {
		return err
	}
	if len(fields) == 1 && !strings.Contains(fields[0], "=") {
		cfg.Cmd = []string{fields[0]}
		cfg.NoCmd = false
		return nil
	}
	cfg.NoUser = true
	cfg.NoCwd = true
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			return errors.Errorf("invalid value %s", field)
		}
		key := strings.ToLower(parts[0])
		value := parts[1]
		switch key {
		case "args":
			cfg.Cmd = append(cfg.Cmd, maybeJSONArray(value)...)
			cfg.NoCmd = false
		case "entrypoint":
			cfg.Entrypoint = append(cfg.Entrypoint, maybeJSONArray(value)...)
			if cfg.Cmd == nil {
				cfg.Cmd = []string{}
				cfg.NoCmd = false
			}
		case "env":
			cfg.Env = append(cfg.Env, maybeJSONArray(value)...)
		case "user":
			cfg.User = value
			cfg.NoUser = false
		case "cwd":
			cfg.Cwd = value
			cfg.NoCwd = false
		case "tty":
			cfg.Tty, err = strconv.ParseBool(value)
			if err != nil {
				return errors.Errorf("failed to parse tty: %v", err)
			}
		default:
			return errors.Errorf("unknown key %q", key)
		}
	}
	return nil
}

func maybeJSONArray(v string) []string {
	var list []string
	if err := json.Unmarshal([]byte(v), &list); err == nil {
		return list
	}
	return []string{v}
}

func callAlias(target *string, value string) cobrautil.BoolFuncValue {
	return func(s string) error {
		v, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}

		if v {
			*target = value
		}
		return nil
	}
}

// timeBuildCommand will start a timer for timing the build command. It records the time when the returned
// function is invoked into a metric.
func timeBuildCommand(mp metric.MeterProvider, attrs attribute.Set) func(err error) {
	meter := metricutil.Meter(mp)
	counter, _ := meter.Float64Counter("command.time",
		metric.WithDescription("Measures the duration of the build command."),
		metric.WithUnit("ms"),
	)

	start := time.Now()
	return func(err error) {
		dur := float64(time.Since(start)) / float64(time.Millisecond)
		extraAttrs := attribute.NewSet()
		if err != nil {
			extraAttrs = attribute.NewSet(
				attribute.String("error.type", otelErrorType(err)),
			)
		}
		counter.Add(context.Background(), dur,
			metric.WithAttributeSet(attrs),
			metric.WithAttributeSet(extraAttrs),
		)
	}
}

// otelErrorType returns an attribute for the error type based on the error category.
// If nil, this function returns an invalid attribute.
func otelErrorType(err error) string {
	name := "generic"
	if errors.Is(err, context.Canceled) {
		name = "canceled"
	}
	return name
}

const defaultTargetName = "default"

type BuildOptions struct {
	ContextPath            string
	DockerfileName         string
	CallFunc               *buildflags.CallFunc
	NamedContexts          map[string]string
	Allow                  []string
	Attests                buildflags.Attests
	BuildArgs              map[string]string
	CacheFrom              []*buildflags.CacheOptionsEntry
	CacheTo                []*buildflags.CacheOptionsEntry
	CgroupParent           string
	Exports                []*buildflags.ExportEntry
	ExtraHosts             []string
	Labels                 map[string]string
	NetworkMode            string
	NoCacheFilter          []string
	Platforms              []string
	Secrets                buildflags.Secrets
	ShmSize                int64
	SSH                    []*buildflags.SSH
	Tags                   []string
	Target                 string
	Ulimits                *dockeropts.UlimitOpt
	Builder                string
	NoCache                bool
	Pull                   bool
	ExportPush             bool
	ExportLoad             bool
	SourcePolicy           *sourcepolicy.Policy
	Ref                    string
	GroupRef               string
	Annotations            []string
	ProvenanceResponseMode string
}

// RunBuild runs the specified build and returns the result.
func RunBuild(ctx context.Context, dockerCli command.Cli, in *BuildOptions, inStream io.Reader, progress progress.Writer, bh *build.Handler) (*client.SolveResponse, *build.Inputs, error) {
	if in.NoCache && len(in.NoCacheFilter) > 0 {
		return nil, nil, errors.Errorf("--no-cache and --no-cache-filter cannot currently be used together")
	}

	contexts := map[string]build.NamedContext{}
	for name, path := range in.NamedContexts {
		contexts[name] = build.NamedContext{Path: path}
	}

	opts := build.Options{
		Inputs: build.Inputs{
			ContextPath:    in.ContextPath,
			DockerfilePath: in.DockerfileName,
			InStream:       build.NewSyncMultiReader(inStream),
			NamedContexts:  contexts,
		},
		Ref:                    in.Ref,
		BuildArgs:              in.BuildArgs,
		CgroupParent:           in.CgroupParent,
		ExtraHosts:             in.ExtraHosts,
		Labels:                 in.Labels,
		NetworkMode:            in.NetworkMode,
		NoCache:                in.NoCache,
		NoCacheFilter:          in.NoCacheFilter,
		Pull:                   in.Pull,
		ShmSize:                dockeropts.MemBytes(in.ShmSize),
		Tags:                   in.Tags,
		Target:                 in.Target,
		Ulimits:                in.Ulimits,
		GroupRef:               in.GroupRef,
		ProvenanceResponseMode: confutil.ParseMetadataProvenance(in.ProvenanceResponseMode),
	}

	platforms, err := platformutil.Parse(in.Platforms)
	if err != nil {
		return nil, nil, err
	}
	opts.Platforms = platforms

	dockerConfig := dockerCli.ConfigFile()
	opts.Session = append(opts.Session, authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		ConfigFile: dockerConfig,
	}))

	secrets, err := build.CreateSecrets(in.Secrets)
	if err != nil {
		return nil, nil, err
	}
	opts.Session = append(opts.Session, secrets)

	sshSpecs := in.SSH
	if len(sshSpecs) == 0 && buildflags.IsGitSSH(in.ContextPath) {
		sshSpecs = append(sshSpecs, &buildflags.SSH{ID: "default"})
	}
	ssh, err := build.CreateSSH(sshSpecs)
	if err != nil {
		return nil, nil, err
	}
	opts.Session = append(opts.Session, ssh)

	outputs, _, err := build.CreateExports(in.Exports)
	if err != nil {
		return nil, nil, err
	}
	if in.ExportPush {
		var pushUsed bool
		for i := range outputs {
			if outputs[i].Type == client.ExporterImage {
				outputs[i].Attrs["push"] = "true"
				pushUsed = true
			}
		}
		if !pushUsed {
			outputs = append(outputs, client.ExportEntry{
				Type: client.ExporterImage,
				Attrs: map[string]string{
					"push": "true",
				},
			})
		}
	}
	if in.ExportLoad {
		var loadUsed bool
		for i := range outputs {
			if outputs[i].Type == client.ExporterDocker {
				if _, ok := outputs[i].Attrs["dest"]; !ok {
					loadUsed = true
					break
				}
			}
		}
		if !loadUsed {
			outputs = append(outputs, client.ExportEntry{
				Type:  client.ExporterDocker,
				Attrs: map[string]string{},
			})
		}
	}

	annotations, err := buildflags.ParseAnnotations(in.Annotations)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parse annotations")
	}

	for _, o := range outputs {
		for k, v := range annotations {
			o.Attrs[k.String()] = v
		}
	}

	opts.Exports = outputs

	opts.CacheFrom = build.CreateCaches(in.CacheFrom)
	opts.CacheTo = build.CreateCaches(in.CacheTo)

	opts.Attests = in.Attests.ToMap()

	opts.SourcePolicy = in.SourcePolicy

	allow, err := buildflags.ParseEntitlements(in.Allow)
	if err != nil {
		return nil, nil, err
	}
	opts.Allow = allow

	if in.CallFunc != nil {
		opts.CallFunc = &build.CallFunc{
			Name:         in.CallFunc.Name,
			Format:       in.CallFunc.Format,
			IgnoreStatus: in.CallFunc.IgnoreStatus,
		}
	}

	// key string used for kubernetes "sticky" mode
	contextPathHash, err := filepath.Abs(in.ContextPath)
	if err != nil {
		contextPathHash = in.ContextPath
	}

	b, err := builder.New(dockerCli,
		builder.WithName(in.Builder),
		builder.WithContextPathHash(contextPathHash),
	)
	if err != nil {
		return nil, nil, err
	}
	if err = updateLastActivity(dockerCli, b.NodeGroup); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to update builder last activity time")
	}
	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return nil, nil, err
	}

	var inputs *build.Inputs
	buildOptions := map[string]build.Options{defaultTargetName: opts}
	resp, err := build.BuildWithResultHandler(ctx, nodes, buildOptions, dockerutil.NewClient(dockerCli), confutil.NewConfig(dockerCli), progress, bh)
	err = wrapBuildError(err, false)
	if err != nil {
		return nil, nil, err
	}
	if i, ok := buildOptions[defaultTargetName]; ok {
		inputs = &i.Inputs
	}
	return resp[defaultTargetName], inputs, nil
}
