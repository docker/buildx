package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	bootstrap bool
	builder   string
}

func runInspect(dockerCli command.Cli, in inspectOptions) error {
	ctx := appcontext.Context()

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	var ng *store.NodeGroup

	if in.builder != "" {
		ng, err = storeutil.GetNodeGroup(txn, dockerCli, in.builder)
		if err != nil {
			return err
		}
	} else {
		ng, err = storeutil.GetCurrentInstance(txn, dockerCli)
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

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err = loadNodeGroupData(timeoutCtx, dockerCli, ngi)

	var bootNgi *nginfo
	if in.bootstrap {
		var ok bool
		ok, err = boot(ctx, ngi)
		if err != nil {
			return err
		}
		bootNgi = ngi
		if ok {
			ngi = &nginfo{ng: ng}
			err = loadNodeGroupData(ctx, dockerCli, ngi)
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", ngi.ng.Name)
	fmt.Fprintf(w, "Driver:\t%s\n", ngi.ng.Driver)
	if err != nil {
		fmt.Fprintf(w, "Error:\t%s\n", err.Error())
	} else if ngi.err != nil {
		fmt.Fprintf(w, "Error:\t%s\n", ngi.err.Error())
	}
	if err == nil {
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
			} else if bootNgi != nil && len(bootNgi.drivers) > i && bootNgi.drivers[i].err != nil {
				fmt.Fprintf(w, "Error:\t%s\n", bootNgi.drivers[i].err.Error())
			} else {
				fmt.Fprintf(w, "Status:\t%s\n", ngi.drivers[i].info.Status)
				if len(n.Flags) > 0 {
					fmt.Fprintf(w, "Flags:\t%s\n", strings.Join(n.Flags, " "))
				}
				fmt.Fprintf(w, "Platforms:\t%s\n", strings.Join(platformutil.FormatInGroups(n.Platforms, ngi.drivers[i].platforms), ", "))
			}
		}
	}

	w.Flush()

	return nil
}

func inspectCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [NAME]",
		Short: "Inspect current builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			if len(args) > 0 {
				options.builder = args[0]
			}
			return runInspect(dockerCli, options)
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.bootstrap, "bootstrap", false, "Ensure builder has booted before inspecting")

	_ = flags

	return cmd
}
