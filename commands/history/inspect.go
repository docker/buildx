package history

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/go-csvvalue"
	"google.golang.org/grpc/codes"
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

	log.Printf("rec %+v", rec)
	log.Printf("st %+v", st)

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
				dockerfile = rel
			}
		}
		if context != "" {
			if rel, err := filepath.Rel(wd, context); err == nil {
				context = rel
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

	fmt.Fprintf(tw, "Started:\t%s\n", rec.CreatedAt.AsTime().Format("2006-01-02 15:04:05"))
	var duration time.Duration
	var status string
	if rec.CompletedAt != nil {
		duration = rec.CompletedAt.AsTime().Sub(rec.CreatedAt.AsTime())
	} else {
		duration = rec.currentTimestamp.Sub(rec.CreatedAt.AsTime())
		status = " (running)"
	}
	fmt.Fprintf(tw, "Duration:\t%s%s\n", formatDuration(duration), status)
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
		if strings.HasPrefix(k, "vcs:") || strings.HasPrefix(k, "build-arg:") || strings.HasPrefix(k, "label:") || strings.HasPrefix(k, "context:") {
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

	// exporters (image)
	// commands
	// error
	// materials

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

	// flags := cmd.Flags()

	return cmd
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
