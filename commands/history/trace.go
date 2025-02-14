package history

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/console"
	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/otelutil"
	"github.com/docker/buildx/util/otelutil/jaeger"
	"github.com/docker/cli/cli/command"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/browser"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	jaegerui "github.com/tonistiigi/jaeger-ui-rest"
)

type traceOptions struct {
	builder string
	ref     string
	addr    string
	compare string
}

func loadTrace(ctx context.Context, ref string, nodes []builder.Node) (string, []byte, error) {
	var offset *int
	if strings.HasPrefix(ref, "^") {
		off, err := strconv.Atoi(ref[1:])
		if err != nil {
			return "", nil, errors.Wrapf(err, "invalid offset %q", ref)
		}
		offset = &off
		ref = ""
	}

	recs, err := queryRecords(ctx, ref, nodes)
	if err != nil {
		return "", nil, err
	}

	var rec *historyRecord

	if ref == "" {
		slices.SortFunc(recs, func(a, b historyRecord) int {
			return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
		})
		for _, r := range recs {
			if r.CompletedAt != nil {
				if offset != nil {
					if *offset > 0 {
						*offset--
						continue
					}
				}
				rec = &r
				break
			}
		}
		if offset != nil && *offset > 0 {
			return "", nil, errors.Errorf("no completed build found with offset %d", *offset)
		}
	} else {
		rec = &recs[0]
	}
	if rec == nil {
		if ref == "" {
			return "", nil, errors.New("no records found")
		}
		return "", nil, errors.Errorf("no record found for ref %q", ref)
	}

	if rec.CompletedAt == nil {
		return "", nil, errors.Errorf("build %q is not completed, only completed builds can be traced", rec.Ref)
	}

	if rec.Trace == nil {
		// build is complete but no trace yet. try to finalize the trace
		time.Sleep(1 * time.Second) // give some extra time for last parts of trace to be written

		c, err := rec.node.Driver.Client(ctx)
		if err != nil {
			return "", nil, err
		}
		_, err = c.ControlClient().UpdateBuildHistory(ctx, &controlapi.UpdateBuildHistoryRequest{
			Ref:      rec.Ref,
			Finalize: true,
		})
		if err != nil {
			return "", nil, err
		}

		recs, err := queryRecords(ctx, rec.Ref, []builder.Node{*rec.node})
		if err != nil {
			return "", nil, err
		}

		if len(recs) == 0 {
			return "", nil, errors.Errorf("build record %q was deleted", rec.Ref)
		}

		rec = &recs[0]
		if rec.Trace == nil {
			return "", nil, errors.Errorf("build record %q is missing a trace", rec.Ref)
		}
	}

	c, err := rec.node.Driver.Client(ctx)
	if err != nil {
		return "", nil, err
	}

	store := proxy.NewContentStore(c.ContentClient())

	ra, err := store.ReaderAt(ctx, ocispecs.Descriptor{
		Digest:    digest.Digest(rec.Trace.Digest),
		MediaType: rec.Trace.MediaType,
		Size:      rec.Trace.Size,
	})
	if err != nil {
		return "", nil, err
	}

	spans, err := otelutil.ParseSpanStubs(io.NewSectionReader(ra, 0, ra.Size()))
	if err != nil {
		return "", nil, err
	}

	wrapper := struct {
		Data []jaeger.Trace `json:"data"`
	}{
		Data: spans.JaegerData().Data,
	}

	if len(wrapper.Data) == 0 {
		return "", nil, errors.New("no trace data")
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(wrapper); err != nil {
		return "", nil, err
	}

	return string(wrapper.Data[0].TraceID), buf.Bytes(), nil
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

	traceID, data, err := loadTrace(ctx, opts.ref, nodes)
	if err != nil {
		return err
	}
	srv := jaegerui.NewServer(jaegerui.Config{})
	if err := srv.AddTrace(traceID, bytes.NewReader(data)); err != nil {
		return err
	}
	url := "/trace/" + traceID

	if opts.compare != "" {
		traceIDcomp, data, err := loadTrace(ctx, opts.compare, nodes)
		if err != nil {
			return errors.Wrapf(err, "failed to load trace for %s", opts.compare)
		}
		if err := srv.AddTrace(traceIDcomp, bytes.NewReader(data)); err != nil {
			return err
		}
		url = "/trace/" + traceIDcomp + "..." + traceID
	}

	var term bool
	if _, err := console.ConsoleFromFile(os.Stdout); err == nil {
		term = true
	}

	if !term && opts.compare == "" {
		fmt.Fprintln(dockerCli.Out(), string(data))
		return nil
	}

	ln, err := net.Listen("tcp", opts.addr)
	if err != nil {
		return err
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		browser.OpenURL(url)
	}()

	url = "http://" + ln.Addr().String() + url
	fmt.Fprintf(dockerCli.Err(), "Trace available at %s\n", url)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	err = srv.Serve(ln)
	if err != nil {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
	return err
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
	flags.StringVar(&options.addr, "addr", "127.0.0.1:0", "Address to bind the UI server")
	flags.StringVar(&options.compare, "compare", "", "Compare with another build reference")

	return cmd
}
