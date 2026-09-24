package history

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	controlapi "github.com/moby/buildkit/api/services/control"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
)

type logsOptions struct {
	builder  string
	ref      string
	progress string
}

func runLogs(ctx context.Context, dockerCli command.Cli, opts logsOptions) error {
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

	cl, err := c.ControlClient().Status(ctx, &controlapi.StatusRequest{
		Ref: rec.Ref,
	})
	if err != nil {
		return err
	}
	defer cl.CloseSend()

	mode := progressui.DisplayMode(opts.progress)
	if mode == progressui.AutoMode {
		mode = progressui.PlainMode
	}
	printer, err := progress.NewPrinter(context.WithoutCancel(ctx), os.Stderr, mode)
	if err != nil {
		return err
	}
	defer printer.Wait()

loop0:
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
			ev, err := cl.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break loop0
				}
				return err
			}
			printer.Write(client.NewSolveStatus(ev))
		}
	}

	printerErr := printer.Wait()

	errOut, err := loadErrorOutput(ctx, c, rec)
	if err != nil {
		return err
	}
	printLogError(os.Stderr, mode, errOut)

	return printerErr
}

func printLogError(w io.Writer, mode progressui.DisplayMode, out *errorOutput) {
	if out == nil || mode == progressui.RawJSONMode {
		return
	}

	fmt.Fprintln(w)
	if codes.Code(out.Code) == codes.Canceled {
		fmt.Fprintln(w, "Build canceled")
	} else if out.Message != "" {
		fmt.Fprintf(w, "Error: %s %s\n", codes.Code(out.Code), out.Message)
	}
	printErrorDetails(w, out)
}

func logsCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options logsOptions

	cmd := &cobra.Command{
		Use:   "logs [OPTIONS] [REF]",
		Short: "Print the logs of a build record",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			options.builder = *rootOpts.Builder
			return runLogs(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.progress, "progress", "plain", "Set type of progress output (plain, rawjson, tty)")

	return cmd
}
