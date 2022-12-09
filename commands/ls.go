package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type lsOptions struct {
	quiet   bool
	noTrunc bool
	format  string
}

func runLs(dockerCli command.Cli, in lsOptions) error {
	ctx := appcontext.Context()

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	current, err := storeutil.GetCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}

	builders, err := builder.GetBuilders(dockerCli, txn)
	if err != nil {
		return err
	}

	if !in.quiet {
		timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		eg, _ := errgroup.WithContext(timeoutCtx)
		for _, b := range builders {
			func(b *builder.Builder) {
				eg.Go(func() error {
					_, _ = b.LoadNodes(timeoutCtx, true)
					return nil
				})
			}(b)
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	if hasErrors, err := lsPrint(dockerCli, current, builders, !in.noTrunc, in.quiet, in.format); err != nil {
		return err
	} else if hasErrors {
		_, _ = fmt.Fprintf(dockerCli.Err(), "\n")
		for _, b := range builders {
			if b.Err() != nil {
				_, _ = fmt.Fprintf(dockerCli.Err(), "Cannot load builder %s: %s\n", b.Name, strings.TrimSpace(b.Err().Error()))
			} else {
				for _, d := range b.Nodes() {
					if d.Err != nil {
						_, _ = fmt.Fprintf(dockerCli.Err(), "Failed to get status for %s (%s): %s\n", b.Name, d.Name, strings.TrimSpace(d.Err.Error()))
					}
				}
			}
		}
	}

	return nil
}

func lsCmd(dockerCli command.Cli) *cobra.Command {
	var options lsOptions

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List builder instances",
		Args:  cli.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(dockerCli, options)
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Only display builder names")
	flags.BoolVar(&options.noTrunc, "no-trunc", false, "Do not truncate output")
	flags.StringVar(&options.format, "format", formatter.TableFormatKey, "Format the output")

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}
