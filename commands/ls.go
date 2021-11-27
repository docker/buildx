package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/builderutil"
	"github.com/docker/buildx/util/cobrautil"
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

	builders, err := builderutil.GetBuilders(dockerCli, txn)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	eg, _ := errgroup.WithContext(timeoutCtx)
	for _, b := range builders {
		func(b *builderutil.Builder) {
			eg.Go(func() error {
				err = b.LoadDrivers(timeoutCtx, true, "")
				if b.Err == nil && err != nil {
					b.Err = err
				}
				return nil
			})
		}(b)
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "NAME/NODE\tDRIVER/ENDPOINT\tSTATUS\tPLATFORMS\n")

	for _, b := range builders {
		if b.NodeGroup.Current {
			b.NodeGroup.Name += " *"
		}
		printngi(w, b)
	}

	w.Flush()

	return nil
}

func printngi(w io.Writer, b *builderutil.Builder) {
	var err string
	if b.Err != nil {
		err = b.Err.Error()
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t\n", b.NodeGroup.Name, b.NodeGroup.Driver, err)
	if b.Err == nil {
		for idx, n := range b.NodeGroup.Nodes {
			d := b.Drivers[idx]
			var err string
			if d.Err != nil {
				err = d.Err.Error()
			}
			var status string
			if d.Info != nil {
				status = d.Info.Status.String()
			}
			if err != "" {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", n.Name, n.Endpoint, err)
			} else {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", n.Name, n.Endpoint, status, strings.Join(platformutil.FormatInGroups(n.Platforms, d.Platforms), ", "))
			}
		}
	}
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

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}
