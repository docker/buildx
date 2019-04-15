package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/buildx/build"
	"github.com/tonistiigi/buildx/driver"
	"github.com/tonistiigi/buildx/store"
	"github.com/tonistiigi/buildx/util/progress"
	"golang.org/x/sync/errgroup"
)

type inspectOptions struct {
	bootstrap bool
}

type dinfo struct {
	di        *build.DriverInfo
	info      *driver.Info
	platforms []string
	err       error
}

type nginfo struct {
	ng      *store.NodeGroup
	drivers []dinfo
	err     error
}

func runInspect(dockerCli command.Cli, in inspectOptions, args []string) error {
	ctx := appcontext.Context()

	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	var ng *store.NodeGroup

	if len(args) > 0 {
		ng, err = getNodeGroup(txn, dockerCli, args[0])
		if err != nil {
			return err
		}
	} else {
		ng, err = getCurrentInstance(txn, dockerCli)
		if err != nil {
			return err
		}
	}

	if ng == nil {
		ng = &store.NodeGroup{
			Name: "default",
			Nodes: []store.Node{{
				Name:     "default",
				Endpoint: "default",
			}},
		}
	}

	ngi := &nginfo{ng: ng}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err = loadNodeGroupData(timeoutCtx, dockerCli, ngi)

	if in.bootstrap {
		if err := boot(ctx, ngi); err != nil {
			return err
		}
		ngi = &nginfo{ng: ng}
		err = loadNodeGroupData(ctx, dockerCli, ngi)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", ngi.ng.Name)
	fmt.Fprintf(w, "Driver:\t%s\n", ngi.ng.Driver)
	if err != nil {
		fmt.Fprintf(w, "Error:\t%s\n", err.Error())
	} else if ngi.err != nil {
		fmt.Fprintf(w, "Error:\t%s\n", ngi.err.Error())
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Nodes:")

	for i, n := range ngi.ng.Nodes {
		if i != 0 {
			fmt.Fprintln(w, "")
		}
		fmt.Fprintf(w, "Name:\t%s\n", n.Name)
		fmt.Fprintf(w, "Endpoint:\t%s\n", n.Endpoint)
		if err := ngi.drivers[i].di.Err; err != nil {
			fmt.Fprintf(w, "Error:\t%s\n", err.Error())
		} else if err := ngi.drivers[i].err; err != nil {
			fmt.Fprintf(w, "Error:\t%s\n", err.Error())
		} else {
			fmt.Fprintf(w, "Status:\t%s\n", ngi.drivers[i].info.Status)
			fmt.Fprintf(w, "Platforms:\t%s\n", strings.Join(append(n.Platforms, ngi.drivers[i].platforms...), ", "))
		}
	}

	w.Flush()

	return nil
}

func inspectCmd(dockerCli command.Cli) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [NAME]",
		Short: "Inspect current builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(dockerCli, options, args)
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.bootstrap, "bootstrap", false, "Ensure builder has booted before inspecting")

	_ = flags

	return cmd
}

func boot(ctx context.Context, ngi *nginfo) error {
	toBoot := make([]int, 0, len(ngi.drivers))
	for i, d := range ngi.drivers {
		if d.err != nil || d.di.Err != nil || d.di.Driver == nil || d.info == nil {
			continue
		}
		if d.info.Status != driver.Running {
			toBoot = append(toBoot, i)
		}
	}
	if len(toBoot) == 0 {
		return nil
	}

	pw := progress.NewPrinter(context.TODO(), os.Stderr, "auto")

	mw := progress.NewMultiWriter(pw)

	eg, _ := errgroup.WithContext(ctx)
	for _, idx := range toBoot {
		func(idx int) {
			eg.Go(func() error {
				pw := mw.WithPrefix(ngi.ng.Nodes[idx].Name, len(toBoot) > 1)
				_, _, err := driver.Boot(ctx, ngi.drivers[idx].di.Driver, pw)
				if err != nil {
					ngi.drivers[idx].err = err
				}
				close(pw.Status())
				<-pw.Done()
				return nil
			})
		}(idx)
	}

	return eg.Wait()
}
