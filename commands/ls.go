package commands

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
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

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	ll, err := txn.List()
	if err != nil {
		return err
	}

	builders := make([]*nginfo, len(ll))
	for i, ng := range ll {
		builders[i] = &nginfo{ng: ng}
	}

	contexts, err := dockerCli.ContextStore().List()
	if err != nil {
		return err
	}
	sort.Slice(contexts, func(i, j int) bool {
		return contexts[i].Name < contexts[j].Name
	})
	for _, c := range contexts {
		ngi := &nginfo{ng: &store.NodeGroup{
			Name: c.Name,
			Nodes: []store.Node{{
				Name:     c.Name,
				Endpoint: c.Name,
			}},
		}}
		// if a context has the same name as an instance from the store, do not
		// add it to the builders list. An instance from the store takes
		// precedence over context builders.
		if hasNodeGroup(builders, ngi) {
			continue
		}
		builders = append(builders, ngi)
	}

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
	current, err := storeutil.GetCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}
	if current != nil {
		currentName = current.Name
		if current.Name == "default" {
			currentName = current.Nodes[0].Endpoint
		}
	}

	w := tabwriter.NewWriter(dockerCli.Out(), 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "NAME/NODE\tDRIVER/ENDPOINT\tSTATUS\tBUILDKIT\tPLATFORMS\n")

	currentSet := false
	printErr := false
	for _, b := range builders {
		if !currentSet && b.ng.Name == currentName {
			b.ng.Name += " *"
			currentSet = true
		}
		if ok := printngi(w, b); !ok {
			printErr = true
		}
	}

	w.Flush()

	if printErr {
		_, _ = fmt.Fprintf(dockerCli.Err(), "\n")
		for _, b := range builders {
			if b.err != nil {
				_, _ = fmt.Fprintf(dockerCli.Err(), "Cannot load builder %s: %s\n", b.ng.Name, strings.TrimSpace(b.err.Error()))
			} else {
				for idx, n := range b.ng.Nodes {
					d := b.drivers[idx]
					var nodeErr string
					if d.err != nil {
						nodeErr = d.err.Error()
					} else if d.di.Err != nil {
						nodeErr = d.di.Err.Error()
					}
					if nodeErr != "" {
						_, _ = fmt.Fprintf(dockerCli.Err(), "Failed to get status for %s (%s): %s\n", b.ng.Name, n.Name, strings.TrimSpace(nodeErr))
					}
				}
			}
		}
	}

	return nil
}

func printngi(w io.Writer, ngi *nginfo) (ok bool) {
	ok = true
	var err string
	if ngi.err != nil {
		ok = false
		err = "error"
	}
	fmt.Fprintf(w, "%s\t%s\t%s\t\t\n", ngi.ng.Name, ngi.ng.Driver, err)
	if ngi.err == nil {
		for idx, n := range ngi.ng.Nodes {
			d := ngi.drivers[idx]
			var status string
			if d.info != nil {
				status = d.info.Status.String()
			}
			if d.err != nil || d.di.Err != nil {
				ok = false
				fmt.Fprintf(w, "  %s\t%s\t%s\t\t\n", n.Name, n.Endpoint, "error")
			} else {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n", n.Name, n.Endpoint, status, d.version, strings.Join(platformutil.FormatInGroups(n.Platforms, d.platforms), ", "))
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
	}

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}
