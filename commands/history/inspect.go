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
	"time"

	"github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/cli/cli/command"
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

type inspectOptions struct {
	builder string
	ref     string
}

func runInspect(ctx context.Context, dockerCli command.Cli, opts inspectOptions) error {
	b, err := builder.New(dockerCli, builder.WithName(opts.builder))
	if err != nil {
		return err
	}

	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Err != nil {
			return node.Err
		}
	}

	recs, err := queryRecords(ctx, opts.ref, nodes)
	if err != nil {
		return err
	}

	if len(recs) == 0 {
		if opts.ref == "" {
			return errors.New("no records found")
		}
		return errors.Errorf("no record found for ref %q", opts.ref)
	}

	if opts.ref == "" {
		slices.SortFunc(recs, func(a, b historyRecord) int {
			return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
		})
	}

	rec := &recs[0]

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}
	st, _ := ls.ReadRef(rec.node.Builder, rec.node.Name, rec.Ref)

	tw := tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)

	attrs := rec.FrontendAttrs
	delete(attrs, "frontend.caps")

	writeAttr := func(k, name string, f func(v string) (string, bool)) {
		if v, ok := attrs[k]; ok {
			if f != nil {
				v, ok = f(v)
			}
			if ok {
				fmt.Fprintf(tw, "%s:\t%s\n", name, v)
			}
		}
		delete(attrs, k)
	}

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

	if context != "" {
		fmt.Fprintf(tw, "Context:\t%s\n", context)
	}
	if dockerfile != "" {
		fmt.Fprintf(tw, "Dockerfile:\t%s\n", dockerfile)
	}
	if _, ok := attrs["context"]; !ok {
		if src, ok := attrs["vcs:source"]; ok {
			fmt.Fprintf(tw, "VCS Repository:\t%s\n", src)
		}
		if rev, ok := attrs["vcs:revision"]; ok {
			fmt.Fprintf(tw, "VCS Revision:\t%s\n", rev)
		}
	}

	writeAttr("target", "Target", nil)
	writeAttr("platform", "Platform", func(v string) (string, bool) {
		return tryParseValue(v, func(v string) (string, error) {
			var pp []string
			for _, v := range strings.Split(v, ",") {
				p, err := platforms.Parse(v)
				if err != nil {
					return "", err
				}
				pp = append(pp, platforms.FormatAll(platforms.Normalize(p)))
			}
			return strings.Join(pp, ", "), nil
		}), true
	})
	writeAttr("build-arg:BUILDKIT_CONTEXT_KEEP_GIT_DIR", "Keep Git Dir", func(v string) (string, bool) {
		return tryParseValue(v, func(v string) (string, error) {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return "", err
			}
			return strconv.FormatBool(b), nil
		}), true
	})

	tw.Flush()

	fmt.Fprintln(dockerCli.Out())

	printTable(dockerCli.Out(), attrs, "context:", "Named Context")

	tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)

	fmt.Fprintf(tw, "Started:\t%s\n", rec.CreatedAt.AsTime().Local().Format("2006-01-02 15:04:05"))
	var duration time.Duration
	var statusStr string
	if rec.CompletedAt != nil {
		duration = rec.CompletedAt.AsTime().Sub(rec.CreatedAt.AsTime())
	} else {
		duration = rec.currentTimestamp.Sub(rec.CreatedAt.AsTime())
		statusStr = " (running)"
	}
	fmt.Fprintf(tw, "Duration:\t%s%s\n", formatDuration(duration), statusStr)
	if rec.Error != nil {
		if codes.Code(rec.Error.Code) == codes.Canceled {
			fmt.Fprintf(tw, "Status:\tCanceled\n")
		} else {
			fmt.Fprintf(tw, "Error:\t%s %s\n", codes.Code(rec.Error.Code).String(), rec.Error.Message)
		}
	}
	fmt.Fprintf(tw, "Build Steps:\t%d/%d (%.0f%% cached)\n", rec.NumCompletedSteps, rec.NumTotalSteps, float64(rec.NumCachedSteps)/float64(rec.NumTotalSteps)*100)
	tw.Flush()

	fmt.Fprintln(dockerCli.Out())

	tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)

	writeAttr("force-network-mode", "Network", nil)
	writeAttr("hostname", "Hostname", nil)
	writeAttr("add-hosts", "Extra Hosts", func(v string) (string, bool) {
		return tryParseValue(v, func(v string) (string, error) {
			fields, err := csvvalue.Fields(v, nil)
			if err != nil {
				return "", err
			}
			return strings.Join(fields, ", "), nil
		}), true
	})
	writeAttr("cgroup-parent", "Cgroup Parent", nil)
	writeAttr("image-resolve-mode", "Image Resolve Mode", nil)
	writeAttr("multi-platform", "Force Multi-Platform", nil)
	writeAttr("build-arg:BUILDKIT_MULTI_PLATFORM", "Force Multi-Platform", nil)
	writeAttr("no-cache", "Disable Cache", func(v string) (string, bool) {
		if v == "" {
			return "true", true
		}
		return v, true
	})
	writeAttr("shm-size", "Shm Size", nil)
	writeAttr("ulimit", "Resource Limits", nil)
	writeAttr("build-arg:BUILDKIT_CACHE_MOUNT_NS", "Cache Mount Namespace", nil)
	writeAttr("build-arg:BUILDKIT_DOCKERFILE_CHECK", "Dockerfile Check Config", nil)
	writeAttr("build-arg:SOURCE_DATE_EPOCH", "Source Date Epoch", nil)
	writeAttr("build-arg:SANDBOX_HOSTNAME", "Sandbox Hostname", nil)

	var unusedAttrs []string
	for k := range attrs {
		if strings.HasPrefix(k, "vcs:") || strings.HasPrefix(k, "build-arg:") || strings.HasPrefix(k, "label:") || strings.HasPrefix(k, "context:") || strings.HasPrefix(k, "attest:") {
			continue
		}
		unusedAttrs = append(unusedAttrs, k)
	}
	slices.Sort(unusedAttrs)

	for _, k := range unusedAttrs {
		fmt.Fprintf(tw, "%s:\t%s\n", k, attrs[k])
	}

	tw.Flush()

	fmt.Fprintln(dockerCli.Out())

	printTable(dockerCli.Out(), attrs, "build-arg:", "Build Arg")
	printTable(dockerCli.Out(), attrs, "label:", "Label")

	c, err := rec.node.Driver.Client(ctx)
	if err != nil {
		return err
	}

	store := proxy.NewContentStore(c.ContentClient())

	attachments, err := allAttachments(ctx, store, *rec)
	if err != nil {
		return err
	}

	provIndex := slices.IndexFunc(attachments, func(a attachment) bool {
		return descrType(a.descr) == slsa02.PredicateSLSAProvenance
	})
	if provIndex != -1 {
		prov := attachments[provIndex]

		dt, err := content.ReadBlob(ctx, store, prov.descr)
		if err != nil {
			return errors.Errorf("failed to read provenance %s: %v", prov.descr.Digest, err)
		}

		var pred provenancetypes.ProvenancePredicate
		if err := json.Unmarshal(dt, &pred); err != nil {
			return errors.Errorf("failed to unmarshal provenance %s: %v", prov.descr.Digest, err)
		}

		fmt.Fprintln(dockerCli.Out(), "Materials:")
		tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)
		fmt.Fprintf(tw, "URI\tDIGEST\n")
		for _, m := range pred.Materials {
			fmt.Fprintf(tw, "%s\t%s\n", m.URI, strings.Join(digestSetToDigests(m.Digest), ", "))
		}
		tw.Flush()
		fmt.Fprintln(dockerCli.Out())
	}

	if len(attachments) > 0 {
		fmt.Fprintf(tw, "Attachments:\n")
		tw = tabwriter.NewWriter(dockerCli.Out(), 1, 8, 1, '\t', 0)
		fmt.Fprintf(tw, "DIGEST\tPLATFORM\tTYPE\n")
		for _, a := range attachments {
			p := ""
			if a.platform != nil {
				p = platforms.FormatAll(*a.platform)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", a.descr.Digest, p, descrType(a.descr))
		}
		tw.Flush()
		fmt.Fprintln(dockerCli.Out())
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
		for _, s := range errdefs.Sources(retErr) {
			s.Print(dockerCli.Out())
		}
		fmt.Fprintln(dockerCli.Out())

		var ve *errdefs.VertexError
		if errors.As(retErr, &ve) {
			dgst, err := digest.Parse(ve.Vertex.Digest)
			if err != nil {
				return errors.Wrapf(err, "failed to parse vertex digest %s", ve.Vertex.Digest)
			}
			name, logs, err := loadVertexLogs(ctx, c, rec.Ref, dgst, 16)
			if err != nil {
				return errors.Wrapf(err, "failed to load vertex logs %s", dgst)
			}
			if len(logs) > 0 {
				fmt.Fprintln(dockerCli.Out(), "Logs:")
				fmt.Fprintf(dockerCli.Out(), "> => %s:\n", name)
				for _, l := range logs {
					fmt.Fprintln(dockerCli.Out(), "> "+l)
				}
				fmt.Fprintln(dockerCli.Out())
			}
		}

		if debug.IsEnabled() {
			fmt.Fprintf(dockerCli.Out(), "\n%+v\n", stack.Formatter(retErr))
		} else if len(stack.Traces(retErr)) > 0 {
			fmt.Fprintf(dockerCli.Out(), "Enable --debug to see stack traces for error\n")
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
		Short: "Inspect a build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runInspect(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	cmd.AddCommand(
		attachmentCmd(dockerCli, rootOpts),
	)

	// flags := cmd.Flags()

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

func tryParseValue(s string, f func(string) (string, error)) string {
	v, err := f(s)
	if err != nil {
		return fmt.Sprintf("%s (%v)", s, err)
	}
	return v
}

func printTable(w io.Writer, attrs map[string]string, prefix, title string) {
	var keys []string
	for k := range attrs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, strings.TrimPrefix(k, prefix))
		}
	}
	slices.Sort(keys)

	if len(keys) == 0 {
		return
	}

	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	fmt.Fprintf(tw, "%s\tVALUE\n", strings.ToUpper(title))
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\n", k, attrs[prefix+k])
	}
	tw.Flush()
	fmt.Fprintln(w)
}

func digestSetToDigests(ds slsa.DigestSet) []string {
	var out []string
	for k, v := range ds {
		out = append(out, fmt.Sprintf("%s:%s", k, v))
	}
	return out
}
