package commands

import (
	"io"
	"net"
	"os"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/build"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/moby/buildkit/util/progress/progressui"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type stdioOptions struct {
	builder  string
	platform string
	progress string
}

func runDialStdio(dockerCli command.Cli, opts stdioOptions) error {
	ctx := appcontext.Context()

	contextPathHash, _ := os.Getwd()
	b, err := builder.New(dockerCli,
		builder.WithName(opts.builder),
		builder.WithContextPathHash(contextPathHash),
	)
	if err != nil {
		return err
	}

	if err = updateLastActivity(dockerCli, b.NodeGroup); err != nil {
		return errors.Wrapf(err, "failed to update builder last activity time")
	}
	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}

	printer, err := progress.NewPrinter(ctx, os.Stderr, progressui.DisplayMode(opts.progress), progress.WithPhase("dial-stdio"), progress.WithDesc("builder: "+b.Name, "builder:"+b.Name))
	if err != nil {
		return err
	}

	var p *v1.Platform
	if opts.platform != "" {
		pp, err := platforms.Parse(opts.platform)
		if err != nil {
			return errors.Wrapf(err, "invalid platform %q", opts.platform)
		}
		p = &pp
	}

	defer printer.Wait()

	return progress.Wrap("Proxying to builder", printer.Write, func(sub progress.SubLogger) error {
		var conn net.Conn

		err := sub.Wrap("Dialing builder", func() error {
			conn, err = build.Dial(ctx, nodes, printer, p)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}

		defer conn.Close()

		go func() {
			<-ctx.Done()
			closeWrite(conn)
		}()

		var eg errgroup.Group

		eg.Go(func() error {
			_, err := io.Copy(conn, os.Stdin)
			closeWrite(conn)
			return err
		})
		eg.Go(func() error {
			_, err := io.Copy(os.Stdout, conn)
			closeRead(conn)
			return err
		})
		return eg.Wait()
	})
}

func closeRead(conn net.Conn) error {
	if c, ok := conn.(interface{ CloseRead() error }); ok {
		return c.CloseRead()
	}
	return conn.Close()
}

func closeWrite(conn net.Conn) error {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		return c.CloseWrite()
	}
	return conn.Close()
}

func dialStdioCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	opts := stdioOptions{}

	cmd := &cobra.Command{
		Use:   "dial-stdio",
		Short: "Proxy current stdio streams to builder instance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builder = rootOpts.builder
			return runDialStdio(dockerCli, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.platform, "platform", os.Getenv("DOCKER_DEFAULT_PLATFORM"), "Target platform: this is used for node selection")
	flags.StringVar(&opts.progress, "progress", "quiet", `Set type of progress output ("auto", "plain", "tty", "rawjson"). Use plain to show container output`)
	return cmd
}
