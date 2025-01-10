package history

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"slices"
	"time"

	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/otelutil"
	"github.com/docker/cli/cli/command"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type traceOptions struct {
	builder       string
	ref           string
	containerName string
}

func runTrace(ctx context.Context, dockerCli command.Cli, opts traceOptions) error {
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

	var rec *historyRecord

	if opts.ref == "" {
		slices.SortFunc(recs, func(a, b historyRecord) int {
			return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
		})
		for _, r := range recs {
			if r.CompletedAt != nil {
				rec = &r
				break
			}
		}
	} else {
		rec = &recs[0]
	}
	if rec == nil {
		if opts.ref == "" {
			return errors.New("no records found")
		}
		return errors.Errorf("no record found for ref %q", opts.ref)
	}

	if rec.CompletedAt == nil {
		return errors.Errorf("build %q is not completed, only completed builds can be traced", rec.Ref)
	}

	if rec.Trace == nil {
		// build is complete but no trace yet. try to finalize the trace
		time.Sleep(1 * time.Second) // give some extra time for last parts of trace to be written

		c, err := rec.node.Driver.Client(ctx)
		if err != nil {
			return err
		}
		_, err = c.ControlClient().UpdateBuildHistory(ctx, &controlapi.UpdateBuildHistoryRequest{
			Ref:      rec.Ref,
			Finalize: true,
		})
		if err != nil {
			return err
		}

		recs, err := queryRecords(ctx, rec.Ref, []builder.Node{*rec.node})
		if err != nil {
			return err
		}

		if len(recs) == 0 {
			return errors.Errorf("build record %q was deleted", rec.Ref)
		}

		rec = &recs[0]
		if rec.Trace == nil {
			return errors.Errorf("build record %q is missing a trace", rec.Ref)
		}
	}

	log.Printf("trace %+v", rec.Trace)

	c, err := rec.node.Driver.Client(ctx)
	if err != nil {
		return err
	}

	store := proxy.NewContentStore(c.ContentClient())

	ra, err := store.ReaderAt(ctx, ocispecs.Descriptor{
		Digest:    digest.Digest(rec.Trace.Digest),
		MediaType: rec.Trace.MediaType,
		Size:      rec.Trace.Size,
	})
	if err != nil {
		return err
	}

	spans, err := otelutil.ParseSpanStubs(io.NewSectionReader(ra, 0, ra.Size()))
	if err != nil {
		return err
	}

	// TODO: try to upload build to Jaeger UI
	jd := spans.JaegerData().Data

	enc := json.NewEncoder(dockerCli.Out())
	enc.SetIndent("", "  ")
	return enc.Encode(jd)
}

func traceCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options traceOptions

	cmd := &cobra.Command{
		Use:   "trace [OPTIONS] [REF]",
		Short: "Show the OpenTelemetry trace of a build record",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runTrace(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.containerName, "container", "", "Container name")

	return cmd
}
