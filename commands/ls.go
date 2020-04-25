package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/store"
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

	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()

	ll, err := txn.List()
	if err != nil {
		return err
	}

	builders := make([]*nginfo, len(ll))
	for i, ng := range ll {
		builders[i] = &nginfo{ng: ng}
	}

	list, err := dockerCli.ContextStore().List()
	if err != nil {
		return err
	}
	ctxbuilders := make([]*nginfo, len(list))
	for i, l := range list {
		ctxbuilders[i] = &nginfo{ng: &store.NodeGroup{
			Name: l.Name,
			Nodes: []store.Node{{
				Name:     l.Name,
				Endpoint: l.Name,
			}},
		}}
	}

	builders = append(builders, ctxbuilders...)

	eg, _ := errgroup.WithContext(ctx)

	for _, b := range builders {
		func(b *nginfo) {
			eg.Go(func() error {
				err = loadNodeGroupData(ctx, dockerCli, b)
				if b.err == nil && err != nil {
					b.err = err
				}
				return nil
			})
		}(b)
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	currentName := "default"
	current, err := getCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}
	if current != nil {
		currentName = current.Name
		if current.Name == "default" {
			currentName = current.Nodes[0].Endpoint
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "NAME/NODE\tDRIVER/ENDPOINT\tSTATUS\tPLATFORMS\n")

	currentSet := false
	for _, b := range builders {
		if !currentSet && b.ng.Name == currentName {
			b.ng.Name += " *"
			currentSet = true
		}
		printngi(w, b)
	}

	w.Flush()

	return nil
}

func printngi(w io.Writer, ngi *nginfo) {
	var err string
	if ngi.err != nil {
		err = ngi.err.Error()
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t\n", ngi.ng.Name, ngi.ng.Driver, err)
	if ngi.err == nil {
		for idx, n := range ngi.ng.Nodes {
			d := ngi.drivers[idx]
			var err string
			if d.err != nil {
				err = d.err.Error()
			} else if d.di.Err != nil {
				err = d.di.Err.Error()
			}
			var status string
			if d.info != nil {
				status = d.info.Status.String()
			}
			if err != "" {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", n.Name, n.Endpoint, err)
			} else {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", n.Name, n.Endpoint, status, strings.Join(platformutil.FormatInGroups(n.Platforms, d.platforms), ", "))
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

	return cmd
}
