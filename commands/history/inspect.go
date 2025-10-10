package history

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/cli/cli/debug"
	slsa "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/common"
	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/solver/errdefs"
	provenancetypes "github.com/moby/buildkit/solver/llbsolver/provenance/types"
	"github.com/moby/buildkit/util/grpcerrors"
	"github.com/moby/buildkit/util/stack"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/go-csvvalue"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	proto "google.golang.org/protobuf/proto"
)

type statusT string

const (
	statusComplete statusT = "completed"
	statusRunning  statusT = "running"
	statusError    statusT = "failed"
	statusCanceled statusT = "canceled"
)

type inspectOptions struct {
	builder string
	ref     string
	format  string
}

type inspectOutput struct {
	Name string `json:",omitempty"`
	Ref  string

	Context       string   `json:",omitempty"`
	Dockerfile    string   `json:",omitempty"`
	VCSRepository string   `json:",omitempty"`
	VCSRevision   string   `json:",omitempty"`
	Target        string   `json:",omitempty"`
	Platform      []string `json:",omitempty"`
	KeepGitDir    bool     `json:",omitempty"`

	NamedContexts []keyValueOutput `json:",omitempty"`

	StartedAt   *time.Time    `json:",omitempty"`
	CompletedAt *time.Time    `json:",omitempty"`
	Duration    time.Duration `json:",omitempty"`
	Status      statusT       `json:",omitempty"`
	Error       *errorOutput  `json:",omitempty"`

	NumCompletedSteps int32
	NumTotalSteps     int32
	NumCachedSteps    int32

	BuildArgs []keyValueOutput `json:",omitempty"`
	Labels    []keyValueOutput `json:",omitempty"`

	Config configOutput

	Materials   []materialOutput   `json:",omitempty"`
	Attachments []attachmentOutput `json:",omitempty"`

	Errors []string `json:",omitempty"`
}

type configOutput struct {
	Network          string   `json:",omitempty"`
	ExtraHosts       []string `json:",omitempty"`
	Hostname         string   `json:",omitempty"`
	CgroupParent     string   `json:",omitempty"`
	ImageResolveMode string   `json:",omitempty"`
	MultiPlatform    bool     `json:",omitempty"`
	NoCache          bool     `json:",omitempty"`
	NoCacheFilter    []string `json:",omitempty"`

	ShmSize               string `json:",omitempty"`
	Ulimit                string `json:",omitempty"`
	CacheMountNS          string `json:",omitempty"`
	DockerfileCheckConfig string `json:",omitempty"`
	SourceDateEpoch       string `json:",omitempty"`
	SandboxHostname       string `json:",omitempty"`

	RestRaw []keyValueOutput `json:",omitempty"`
}

type materialOutput struct {
	URI     string   `json:",omitempty"`
	Digests []string `json:",omitempty"`
}

type attachmentOutput struct {
	Digest   string `json:",omitempty"`
	Platform string `json:",omitempty"`
	Type     string `json:",omitempty"`
}

type errorOutput struct {
	Code    int      `json:",omitempty"`
	Message string   `json:",omitempty"`
	Name    string   `json:",omitempty"`
	Logs    []string `json:",omitempty"`
	Sources []byte   `json:",omitempty"`
	Stack   []byte   `json:",omitempty"`
}

type keyValueOutput struct {
	Name  string `json:",omitempty"`
	Value string `json:",omitempty"`
}

func readAttr[T any](attrs map[string]string, k string, dest *T, f func(v string) (T, bool)) {
	if sv, ok := attrs[k]; ok {
		if f != nil {
			v, ok := f(sv)
			if ok {
				*dest = v
			}
		}
		if d, ok := any(dest).(*string); ok {
			*d = sv
		}
	}
	delete(attrs, k)
}

func runInspect(ctx context.Context, dockerCli command.Cli, opts inspectOptions) error {
	nodes, err := loadNodes(ctx, dockerCli, opts.builder)
	if err != nil {
		return err
	}

	recs, err := queryRecords(ctx, opts.ref, nodes, nil)
	if err != nil {
		return err
	}

	if len(recs) == 0 {
		if opts.ref == "" {
			return errors.New("no records found")
		}
		return errors.Errorf("no record found for ref %q", opts.ref)
	}

	rec := &recs[0]
	c, err := rec.node.Driver.Client(ctx)
	if err != nil {
		return err
	}

	store := proxy.NewContentStore(c.ContentClient())

	var defaultPlatform string
	workers, err := c.ListWorkers(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to list workers")
	}
workers0:
	for _, w := range workers {
		for _, p := range w.Platforms {
			defaultPlatform = platforms.FormatAll(platforms.Normalize(p))
			break workers0
		}
	}

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}
	st, _ := ls.ReadRef(rec.node.Builder, rec.node.Name, rec.Ref)

	attrs := rec.FrontendAttrs
	delete(attrs, "frontend.caps")

	var out inspectOutput

	var context string
	var dockerfile string
	if st != nil {
		context = st.LocalPath
		dockerfile = st.DockerfilePath
		wd, _ := os.Getwd()

		if dockerfile != "" && dockerfile != "-" {
			if rel, err := filepath.Rel(context, dockerfile); err == nil {
				if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					dockerfile = rel
				}
			}
		}
		if context != "" {
			if rel, err := filepath.Rel(wd, context); err == nil {
				if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					context = rel
				}
			}
		}
	}

	if v, ok := attrs["context"]; ok && context == "" {
		delete(attrs, "context")
		context = v
	}
	if dockerfile == "" {
		if v, ok := attrs["filename"]; ok {
			dockerfile = v
			if dfdir, ok := attrs["vcs:localdir:dockerfile"]; ok {
				dockerfile = filepath.Join(dfdir, dockerfile)
			}
		}
	}
	delete(attrs, "filename")

	out.Name = BuildName(rec.FrontendAttrs, st)
	out.Ref = rec.Ref

	out.Context = context
	out.Dockerfile = dockerfile

	if _, ok := attrs["context"]; !ok {
		if src, ok := attrs["vcs:source"]; ok {
			out.VCSRepository = src
		}
		if rev, ok := attrs["vcs:revision"]; ok {
			out.VCSRevision = rev
		}
	}

	readAttr(attrs, "target", &out.Target, nil)

	readAttr(attrs, "platform", &out.Platform, func(v string) ([]string, bool) {
		return tryParseValue(v, &out.Errors, func(v string) ([]string, error) {
			var pp []string
			for v := range strings.SplitSeq(v, ",") {
				p, err := platforms.Parse(v)
				if err != nil {
					return nil, err
				}
				pp = append(pp, platforms.FormatAll(platforms.Normalize(p)))
			}
			if len(pp) == 0 {
				pp = append(pp, defaultPlatform)
			}
			return pp, nil
		})
	})

	readAttr(attrs, "build-arg:BUILDKIT_CONTEXT_KEEP_GIT_DIR", &out.KeepGitDir, func(v string) (bool, bool) {
		return tryParseValue(v, &out.Errors, strconv.ParseBool)
	})

	out.NamedContexts = readKeyValues(attrs, "context:")

	if rec.CreatedAt != nil {
		tm := rec.CreatedAt.AsTime().Local()
		out.StartedAt = &tm
	}
	out.Status = statusRunning

	if rec.CompletedAt != nil {
		tm := rec.CompletedAt.AsTime().Local()
		out.CompletedAt = &tm
		out.Status = statusComplete
	}

	if rec.Error != nil || rec.ExternalError != nil {
		out.Error = &errorOutput{}
		if rec.Error != nil {
			if codes.Code(rec.Error.Code) == codes.Canceled {
				out.Status = statusCanceled
			} else {
				out.Status = statusError
			}
			out.Error.Code = int(codes.Code(rec.Error.Code))
			out.Error.Message = rec.Error.Message
		}
		if rec.ExternalError != nil {
			dt, err := content.ReadBlob(ctx, store, ociDesc(rec.ExternalError))
			if err != nil {
				return errors.Wrapf(err, "failed to read external error %s", rec.ExternalError.Digest)
			}
			var st spb.Status
			if err := proto.Unmarshal(dt, &st); err != nil {
				return errors.Wrapf(err, "failed to unmarshal external error %s", rec.ExternalError.Digest)
			}
			retErr := grpcerrors.FromGRPC(status.ErrorProto(&st))
			var errsources bytes.Buffer
			for _, s := range errdefs.Sources(retErr) {
				s.Print(&errsources)
				errsources.WriteString("\n")
			}
			out.Error.Sources = errsources.Bytes()
			var ve *errdefs.VertexError
			if errors.As(retErr, &ve) {
				dgst, err := digest.Parse(ve.Digest)
				if err != nil {
					return errors.Wrapf(err, "failed to parse vertex digest %s", ve.Digest)
				}
				name, logs, err := loadVertexLogs(ctx, c, rec.Ref, dgst, 16)
				if err != nil {
					return errors.Wrapf(err, "failed to load vertex logs %s", dgst)
				}
				out.Error.Name = name
				out.Error.Logs = logs
			}
			out.Error.Stack = fmt.Appendf(nil, "%+v", stack.Formatter(retErr))
		}
	}

	if out.StartedAt != nil {
		if out.CompletedAt != nil {
			out.Duration = out.CompletedAt.Sub(*out.StartedAt)
		} else {
			out.Duration = rec.currentTimestamp.Sub(*out.StartedAt)
		}
	}

	out.NumCompletedSteps = rec.NumCompletedSteps
	out.NumTotalSteps = rec.NumTotalSteps
	out.NumCachedSteps = rec.NumCachedSteps

	out.BuildArgs = readKeyValues(attrs, "build-arg:")
	out.Labels = readKeyValues(attrs, "label:")

	readAttr(attrs, "force-network-mode", &out.Config.Network, nil)
	readAttr(attrs, "hostname", &out.Config.Hostname, nil)
	readAttr(attrs, "cgroup-parent", &out.Config.CgroupParent, nil)
	readAttr(attrs, "image-resolve-mode", &out.Config.ImageResolveMode, nil)
	readAttr(attrs, "build-arg:BUILDKIT_MULTI_PLATFORM", &out.Config.MultiPlatform, func(v string) (bool, bool) {
		return tryParseValue(v, &out.Errors, strconv.ParseBool)
	})
	readAttr(attrs, "multi-platform", &out.Config.MultiPlatform, func(v string) (bool, bool) {
		return tryParseValue(v, &out.Errors, strconv.ParseBool)
	})
	readAttr(attrs, "no-cache", &out.Config.NoCache, func(v string) (bool, bool) {
		if v == "" {
			return true, true
		}
		return false, false
	})
	readAttr(attrs, "no-cache", &out.Config.NoCacheFilter, func(v string) ([]string, bool) {
		if v == "" {
			return nil, false
		}
		return strings.Split(v, ","), true
	})

	readAttr(attrs, "add-hosts", &out.Config.ExtraHosts, func(v string) ([]string, bool) {
		return tryParseValue(v, &out.Errors, func(v string) ([]string, error) {
			fields, err := csvvalue.Fields(v, nil)
			if err != nil {
				return nil, err
			}
			return fields, nil
		})
	})

	readAttr(attrs, "shm-size", &out.Config.ShmSize, nil)
	readAttr(attrs, "ulimit", &out.Config.Ulimit, nil)
	readAttr(attrs, "build-arg:BUILDKIT_CACHE_MOUNT_NS", &out.Config.CacheMountNS, nil)
	readAttr(attrs, "build-arg:BUILDKIT_DOCKERFILE_CHECK", &out.Config.DockerfileCheckConfig, nil)
	readAttr(attrs, "build-arg:SOURCE_DATE_EPOCH", &out.Config.SourceDateEpoch, nil)
	readAttr(attrs, "build-arg:SANDBOX_HOSTNAME", &out.Config.SandboxHostname, nil)

	var unusedAttrs []keyValueOutput
	for k := range attrs {
		if strings.HasPrefix(k, "vcs:") || strings.HasPrefix(k, "build-arg:") || strings.HasPrefix(k, "label:") || strings.HasPrefix(k, "context:") || strings.HasPrefix(k, "attest:") {
			continue
		}
		unusedAttrs = append(unusedAttrs, keyValueOutput{
			Name:  k,
			Value: attrs[k],
		})
	}
	slices.SortFunc(unusedAttrs, func(a, b keyValueOutput) int {
		return cmp.Compare(a.Name, b.Name)
	})
	out.Config.RestRaw = unusedAttrs

	attachments, err := allAttachments(ctx, store, *rec)
	if err != nil {
		return err
	}

	provIndex := slices.IndexFunc(attachments, func(a attachment) bool {
		return strings.HasPrefix(descrType(a.descr), "https://slsa.dev/provenance/")
	})
	if provIndex != -1 {
		prov := attachments[provIndex]
		predType := descrType(prov.descr)
		dt, err := content.ReadBlob(ctx, store, prov.descr)
		if err != nil {
			return errors.Errorf("failed to read provenance %s: %v", prov.descr.Digest, err)
		}
		var pred *provenancetypes.ProvenancePredicateSLSA1
		if predType == slsa02.PredicateSLSAProvenance {
			var pred02 *provenancetypes.ProvenancePredicateSLSA02
			if err := json.Unmarshal(dt, &pred02); err != nil {
				return errors.Errorf("failed to unmarshal provenance %s: %v", prov.descr.Digest, err)
			}
			pred = pred02.ConvertToSLSA1()
		} else if err := json.Unmarshal(dt, &pred); err != nil {
			return errors.Errorf("failed to unmarshal provenance %s: %v", prov.descr.Digest, err)
		}
		if pred != nil {
			for _, m := range pred.BuildDefinition.ResolvedDependencies {
				out.Materials = append(out.Materials, materialOutput{
					URI:     m.URI,
					Digests: digestSetToDigests(m.Digest),
				})
			}
		}
	}

	if len(attachments) > 0 {
		for _, a := range attachments {
			p := ""
			if a.platform != nil {
				p = platforms.FormatAll(*a.platform)
			}
			out.Attachments = append(out.Attachments, attachmentOutput{
				Digest:   a.descr.Digest.String(),
				Platform: p,
				Type:     descrType(a.descr),
			})
		}
	}

	if opts.format == formatter.JSONFormatKey {
		enc := json.NewEncoder(dockerCli.Out())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	} else if opts.format != formatter.PrettyFormatKey {
		tmpl, err := template.New("inspect").Parse(opts.format)
		if err != nil {
			return errors.Wrapf(err, "failed to parse format template")
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, out); err != nil {
			return errors.Wrapf(err, "failed to execute format template")
		}
		fmt.Fprintln(dockerCli.Out(), buf.String())
		return nil
	}

	tw := tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)

	if out.Name != "" {
		fmt.Fprintf(tw, "Name:\t%s\n", out.Name)
	}
	if opts.ref == "" && out.Ref != "" {
		fmt.Fprintf(tw, "Ref:\t%s\n", out.Ref)
	}
	if out.Context != "" {
		fmt.Fprintf(tw, "Context:\t%s\n", out.Context)
	}
	if out.Dockerfile != "" {
		fmt.Fprintf(tw, "Dockerfile:\t%s\n", out.Dockerfile)
	}
	if out.VCSRepository != "" {
		fmt.Fprintf(tw, "VCS Repository:\t%s\n", out.VCSRepository)
	}
	if out.VCSRevision != "" {
		fmt.Fprintf(tw, "VCS Revision:\t%s\n", out.VCSRevision)
	}

	if out.Target != "" {
		fmt.Fprintf(tw, "Target:\t%s\n", out.Target)
	}

	if len(out.Platform) > 0 {
		fmt.Fprintf(tw, "Platforms:\t%s\n", strings.Join(out.Platform, ", "))
	}

	if out.KeepGitDir {
		fmt.Fprintf(tw, "Keep Git Dir:\t%s\n", strconv.FormatBool(out.KeepGitDir))
	}

	tw.Flush()

	fmt.Fprintln(dockerCli.Out())

	printTable(dockerCli.Out(), out.NamedContexts, "Named Context")

	tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)

	fmt.Fprintf(tw, "Started:\t%s\n", out.StartedAt.Format("2006-01-02 15:04:05"))
	var statusStr string
	if out.Status == statusRunning {
		statusStr = " (running)"
	}
	fmt.Fprintf(tw, "Duration:\t%s%s\n", formatDuration(out.Duration), statusStr)

	switch out.Status {
	case statusError:
		fmt.Fprintf(tw, "Error:\t%s %s\n", codes.Code(rec.Error.Code).String(), rec.Error.Message)
	case statusCanceled:
		fmt.Fprintf(tw, "Status:\tCanceled\n")
	}

	fmt.Fprintf(tw, "Build Steps:\t%d/%d (%.0f%% cached)\n", out.NumCompletedSteps, out.NumTotalSteps, float64(out.NumCachedSteps)/float64(out.NumTotalSteps)*100)
	tw.Flush()

	fmt.Fprintln(dockerCli.Out())

	tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)

	if out.Config.Network != "" {
		fmt.Fprintf(tw, "Network:\t%s\n", out.Config.Network)
	}
	if out.Config.Hostname != "" {
		fmt.Fprintf(tw, "Hostname:\t%s\n", out.Config.Hostname)
	}
	if len(out.Config.ExtraHosts) > 0 {
		fmt.Fprintf(tw, "Extra Hosts:\t%s\n", strings.Join(out.Config.ExtraHosts, ", "))
	}
	if out.Config.CgroupParent != "" {
		fmt.Fprintf(tw, "Cgroup Parent:\t%s\n", out.Config.CgroupParent)
	}
	if out.Config.ImageResolveMode != "" {
		fmt.Fprintf(tw, "Image Resolve Mode:\t%s\n", out.Config.ImageResolveMode)
	}
	if out.Config.MultiPlatform {
		fmt.Fprintf(tw, "Multi-Platform:\t%s\n", strconv.FormatBool(out.Config.MultiPlatform))
	}
	if out.Config.NoCache {
		fmt.Fprintf(tw, "No Cache:\t%s\n", strconv.FormatBool(out.Config.NoCache))
	}
	if len(out.Config.NoCacheFilter) > 0 {
		fmt.Fprintf(tw, "No Cache Filter:\t%s\n", strings.Join(out.Config.NoCacheFilter, ", "))
	}

	if out.Config.ShmSize != "" {
		fmt.Fprintf(tw, "Shm Size:\t%s\n", out.Config.ShmSize)
	}
	if out.Config.Ulimit != "" {
		fmt.Fprintf(tw, "Resource Limits:\t%s\n", out.Config.Ulimit)
	}
	if out.Config.CacheMountNS != "" {
		fmt.Fprintf(tw, "Cache Mount Namespace:\t%s\n", out.Config.CacheMountNS)
	}
	if out.Config.DockerfileCheckConfig != "" {
		fmt.Fprintf(tw, "Dockerfile Check Config:\t%s\n", out.Config.DockerfileCheckConfig)
	}
	if out.Config.SourceDateEpoch != "" {
		fmt.Fprintf(tw, "Source Date Epoch:\t%s\n", out.Config.SourceDateEpoch)
	}
	if out.Config.SandboxHostname != "" {
		fmt.Fprintf(tw, "Sandbox Hostname:\t%s\n", out.Config.SandboxHostname)
	}

	for _, kv := range out.Config.RestRaw {
		fmt.Fprintf(tw, "%s:\t%s\n", kv.Name, kv.Value)
	}

	tw.Flush()

	fmt.Fprintln(dockerCli.Out())

	printTable(dockerCli.Out(), out.BuildArgs, "Build Arg")
	printTable(dockerCli.Out(), out.Labels, "Label")

	if len(out.Materials) > 0 {
		fmt.Fprintln(dockerCli.Out(), "Materials:")
		tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)
		fmt.Fprintf(tw, "URI\tDIGEST\n")
		for _, m := range out.Materials {
			fmt.Fprintf(tw, "%s\t%s\n", m.URI, strings.Join(m.Digests, ", "))
		}
		tw.Flush()
		fmt.Fprintln(dockerCli.Out())
	}

	if len(out.Attachments) > 0 {
		fmt.Fprintf(tw, "Attachments:\n")
		tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)
		fmt.Fprintf(tw, "DIGEST\tPLATFORM\tTYPE\n")
		for _, a := range out.Attachments {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", a.Digest, a.Platform, a.Type)
		}
		tw.Flush()
		fmt.Fprintln(dockerCli.Out())
	}

	if out.Error != nil {
		if out.Error.Sources != nil {
			fmt.Fprint(dockerCli.Out(), string(out.Error.Sources))
		}
		if len(out.Error.Logs) > 0 {
			fmt.Fprintln(dockerCli.Out(), "Logs:")
			fmt.Fprintf(dockerCli.Out(), "> => %s:\n", out.Error.Name)
			for _, l := range out.Error.Logs {
				fmt.Fprintln(dockerCli.Out(), "> "+l)
			}
			fmt.Fprintln(dockerCli.Out())
		}
		if len(out.Error.Stack) > 0 {
			if debug.IsEnabled() {
				fmt.Fprintf(dockerCli.Out(), "\n%s\n", out.Error.Stack)
			} else {
				fmt.Fprintf(dockerCli.Out(), "Enable --debug to see stack traces for error\n")
			}
		}
	}

	fmt.Fprintf(dockerCli.Out(), "Print build logs: docker buildx history logs %s\n", rec.Ref)

	fmt.Fprintf(dockerCli.Out(), "View build in Docker Desktop: %s\n", desktop.BuildURL(fmt.Sprintf("%s/%s/%s", rec.node.Builder, rec.node.Name, rec.Ref)))

	return nil
}

func inspectCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] [REF]",
		Short: "Inspect a build record",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runInspect(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	cmd.AddCommand(
		attachmentCmd(dockerCli, rootOpts),
	)

	flags := cmd.Flags()
	flags.StringVar(&options.format, "format", formatter.PrettyFormatKey, "Format the output")

	return cmd
}

func loadVertexLogs(ctx context.Context, c *client.Client, ref string, dgst digest.Digest, limit int) (string, []string, error) {
	st, err := c.ControlClient().Status(ctx, &controlapi.StatusRequest{
		Ref: ref,
	})
	if err != nil {
		return "", nil, err
	}

	var name string
	var logs []string
	lastState := map[int]int{}

loop0:
	for {
		select {
		case <-ctx.Done():
			st.CloseSend()
			return "", nil, context.Cause(ctx)
		default:
			ev, err := st.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break loop0
				}
				return "", nil, err
			}
			ss := client.NewSolveStatus(ev)
			for _, v := range ss.Vertexes {
				if v.Digest == dgst {
					name = v.Name
					break
				}
			}
			for _, l := range ss.Logs {
				if l.Vertex == dgst {
					parts := bytes.Split(l.Data, []byte("\n"))
					for i, p := range parts {
						var wrote bool
						if i == 0 {
							idx, ok := lastState[l.Stream]
							if ok && idx != -1 {
								logs[idx] = logs[idx] + string(p)
								wrote = true
							}
						}
						if !wrote {
							if len(p) > 0 {
								logs = append(logs, string(p))
							}
							lastState[l.Stream] = len(logs) - 1
						}
						if i == len(parts)-1 && len(p) == 0 {
							lastState[l.Stream] = -1
						}
					}
				}
			}
		}
	}

	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}

	return name, logs, nil
}

type attachment struct {
	platform *ocispecs.Platform
	descr    ocispecs.Descriptor
}

func allAttachments(ctx context.Context, store content.Store, rec historyRecord) ([]attachment, error) {
	var attachments []attachment

	if rec.Result != nil {
		for _, a := range rec.Result.Attestations {
			attachments = append(attachments, attachment{
				descr: ociDesc(a),
			})
		}
		for _, r := range rec.Result.Results {
			attachments = append(attachments, walkAttachments(ctx, store, ociDesc(r), nil)...)
		}
	}

	for key, ri := range rec.Results {
		p, err := platforms.Parse(key)
		if err != nil {
			return nil, err
		}
		for _, a := range ri.Attestations {
			attachments = append(attachments, attachment{
				platform: &p,
				descr:    ociDesc(a),
			})
		}
		for _, r := range ri.Results {
			attachments = append(attachments, walkAttachments(ctx, store, ociDesc(r), &p)...)
		}
	}

	slices.SortFunc(attachments, func(a, b attachment) int {
		pCmp := 0
		if a.platform == nil && b.platform != nil {
			return -1
		} else if a.platform != nil && b.platform == nil {
			return 1
		} else if a.platform != nil && b.platform != nil {
			pCmp = cmp.Compare(platforms.FormatAll(*a.platform), platforms.FormatAll(*b.platform))
		}
		return cmp.Or(
			pCmp,
			cmp.Compare(descrType(a.descr), descrType(b.descr)),
		)
	})

	return attachments, nil
}

func walkAttachments(ctx context.Context, store content.Store, desc ocispecs.Descriptor, platform *ocispecs.Platform) []attachment {
	_, err := store.Info(ctx, desc.Digest)
	if err != nil {
		return nil
	}

	var out []attachment

	if desc.Annotations["vnd.docker.reference.type"] != "attestation-manifest" {
		out = append(out, attachment{platform: platform, descr: desc})
	}

	if desc.MediaType != ocispecs.MediaTypeImageIndex && desc.MediaType != images.MediaTypeDockerSchema2ManifestList {
		return out
	}

	dt, err := content.ReadBlob(ctx, store, desc)
	if err != nil {
		return out
	}

	var idx ocispecs.Index
	if err := json.Unmarshal(dt, &idx); err != nil {
		return out
	}

	for _, d := range idx.Manifests {
		p := platform
		if d.Platform != nil {
			p = d.Platform
		}
		out = append(out, walkAttachments(ctx, store, d, p)...)
	}

	return out
}

func ociDesc(in *controlapi.Descriptor) ocispecs.Descriptor {
	return ocispecs.Descriptor{
		MediaType:   in.MediaType,
		Digest:      digest.Digest(in.Digest),
		Size:        in.Size,
		Annotations: in.Annotations,
	}
}

func descrType(desc ocispecs.Descriptor) string {
	if typ, ok := desc.Annotations["in-toto.io/predicate-type"]; ok {
		return typ
	}
	return desc.MediaType
}

func tryParseValue[T any](s string, errs *[]string, f func(string) (T, error)) (T, bool) {
	v, err := f(s)
	if err != nil {
		errStr := fmt.Sprintf("failed to parse %s: (%v)", s, err)
		*errs = append(*errs, errStr)
	}
	return v, true
}

func printTable(w io.Writer, kvs []keyValueOutput, title string) {
	if len(kvs) == 0 {
		return
	}

	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	fmt.Fprintf(tw, "%s\tVALUE\n", strings.ToUpper(title))
	for _, k := range kvs {
		fmt.Fprintf(tw, "%s\t%s\n", k.Name, k.Value)
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func readKeyValues(attrs map[string]string, prefix string) []keyValueOutput {
	var out []keyValueOutput
	for k, v := range attrs {
		if name, ok := strings.CutPrefix(k, prefix); ok {
			out = append(out, keyValueOutput{
				Name:  name,
				Value: v,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b keyValueOutput) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

func digestSetToDigests(ds slsa.DigestSet) []string {
	var out []string
	for k, v := range ds {
		out = append(out, fmt.Sprintf("%s:%s", k, v))
	}
	return out
}
