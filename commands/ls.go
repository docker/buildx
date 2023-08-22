package commands

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type lsOptions struct {
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

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	eg, _ := errgroup.WithContext(timeoutCtx)
	for _, b := range builders {
		func(b *builder.Builder) {
			eg.Go(func() error {
				_, _ = b.LoadNodes(timeoutCtx, builder.WithData())
				return nil
			})
		}(b)
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	w := tabwriter.NewWriter(dockerCli.Out(), 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "NAME/NODE\tDRIVER/ENDPOINT\tSTATUS\tBUILDKIT\tPLATFORMS\n")

	printErr := false
	for _, b := range builders {
		if current.Name == b.Name {
			b.Name += " *"
		}
		if ok := printBuilder(w, b); !ok {
			printErr = true
		}
	}

	w.Flush()

	if printErr {
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

func printBuilder(w io.Writer, b *builder.Builder) (ok bool) {
	ok = true
	var err string
	if b.Err() != nil {
		ok = false
		err = "error"
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t\t\n", b.Name, b.Driver, err)
	if b.Err() == nil {
		for _, n := range b.Nodes() {
			var status string
			if n.DriverInfo != nil {
				status = n.DriverInfo.Status.String()
			}
			if n.Err != nil {
				ok = false
				fmt.Fprintf(w, "  %s\t%s\t%s\t\t\n", n.Name, n.Endpoint, "error")
			} else {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", n.Name, n.Endpoint, status, n.Version, strings.Join(platformutil.FormatInGroups(n.Node.Platforms, n.Platforms), ", "))
			}
		}
	}
	return
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
		ValidArgsFunction: completion.Disable,
	}

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}
