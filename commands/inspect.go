package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/builderutil"
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

	builder, err := builderutil.New(dockerCli, txn, in.builder)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err = builder.LoadDrivers(timeoutCtx, true, "")

	var bootBuilder *builderutil.Builder
	if in.bootstrap {
		var ok bool
		ok, err = builder.Boot(ctx)
		if err != nil {
			return err
		}
		bootBuilder = builder
		if ok {
			err = builder.LoadDrivers(timeoutCtx, true, "")
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", builder.NodeGroup.Name)
	fmt.Fprintf(w, "Driver:\t%s\n", builder.NodeGroup.Driver)
	if err != nil {
		fmt.Fprintf(w, "Error:\t%s\n", err.Error())
	} else if builder.Err != nil {
		fmt.Fprintf(w, "Error:\t%s\n", builder.Err.Error())
	}
	if err == nil {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Nodes:")

		for i, n := range builder.NodeGroup.Nodes {
			if i != 0 {
				fmt.Fprintln(w, "")
			}
			fmt.Fprintf(w, "Name:\t%s\n", n.Name)
			fmt.Fprintf(w, "Endpoint:\t%s\n", n.Endpoint)
			if err := builder.Drivers[i].Err; err != nil {
				fmt.Fprintf(w, "Error:\t%s\n", err.Error())
			} else if bootBuilder != nil && len(bootBuilder.Drivers) > i && bootBuilder.Drivers[i].Err != nil {
				fmt.Fprintf(w, "Error:\t%s\n", bootBuilder.Drivers[i].Err.Error())
			} else {
				fmt.Fprintf(w, "Status:\t%s\n", builder.Drivers[i].Info.Status)
				if len(n.Flags) > 0 {
					fmt.Fprintf(w, "Flags:\t%s\n", strings.Join(n.Flags, " "))
				}
				fmt.Fprintf(w, "Platforms:\t%s\n", strings.Join(platformutil.FormatInGroups(n.Platforms, builder.Drivers[i].Platforms), ", "))
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

	return cmd
}
