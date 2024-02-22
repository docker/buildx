package commands

import (
	"bytes"
	"context"
	"fmt"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/spf13/cobra"
)

type createOptions struct {
	name                string
	driver              string
	nodeName            string
	platform            []string
	actionAppend        bool
	actionLeave         bool
	use                 bool
	driverOpts          []string
	buildkitdFlags      string
	buildkitdConfigFile string
	bootstrap           bool
	// upgrade      bool // perform upgrade of the driver
}

func runCreate(ctx context.Context, dockerCli command.Cli, in createOptions, args []string) error {
	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	// Ensure the file lock gets released no matter what happens.
	defer release()

	if in.actionLeave {
		return builder.Leave(ctx, txn, dockerCli, builder.LeaveOpts{
			Name:     in.name,
			NodeName: in.nodeName,
		})
	}

	var ep string
	if len(args) > 0 {
		ep = args[0]
	}

	b, err := builder.Create(ctx, txn, dockerCli, builder.CreateOpts{
		Name:                in.name,
		Driver:              in.driver,
		NodeName:            in.nodeName,
		Platforms:           in.platform,
		DriverOpts:          in.driverOpts,
		BuildkitdFlags:      in.buildkitdFlags,
		BuildkitdConfigFile: in.buildkitdConfigFile,
		Use:                 in.use,
		Endpoint:            ep,
		Append:              in.actionAppend,
	})
	if err != nil {
		return err
	}

	// The store is no longer used from this point.
	// Release it so we aren't holding the file lock during the boot.
	release()

	if in.bootstrap {
		if _, err = b.Boot(ctx); err != nil {
			return err
		}
	}

	fmt.Printf("%s\n", b.Name)
	return nil
}

func createCmd(dockerCli command.Cli) *cobra.Command {
	var options createOptions

	var drivers bytes.Buffer
	for _, d := range driver.GetFactories(true) {
		if len(drivers.String()) > 0 {
			drivers.WriteString(", ")
		}
		drivers.WriteString(fmt.Sprintf(`"%s"`, d.Name()))
	}

	cmd := &cobra.Command{
		Use:   "create [OPTIONS] [CONTEXT|ENDPOINT]",
		Short: "Create a new builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), dockerCli, options, args)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()

	flags.StringVar(&options.name, "name", "", "Builder instance name")
	flags.StringVar(&options.driver, "driver", "", fmt.Sprintf("Driver to use (available: %s)", drivers.String()))
	flags.StringVar(&options.nodeName, "node", "", "Create/modify node with given name")
	flags.StringArrayVar(&options.platform, "platform", []string{}, "Fixed platforms for current node")
	flags.StringArrayVar(&options.driverOpts, "driver-opt", []string{}, "Options for the driver")
	flags.StringVar(&options.buildkitdFlags, "buildkitd-flags", "", "BuildKit daemon flags")

	// we allow for both "--config" and "--buildkitd-config", although the latter is the recommended way to avoid ambiguity.
	flags.StringVar(&options.buildkitdConfigFile, "buildkitd-config", "", "BuildKit daemon config file")
	flags.StringVar(&options.buildkitdConfigFile, "config", "", "BuildKit daemon config file")
	flags.MarkHidden("config")

	flags.BoolVar(&options.bootstrap, "bootstrap", false, "Boot builder after creation")
	flags.BoolVar(&options.actionAppend, "append", false, "Append a node to builder instead of changing it")
	flags.BoolVar(&options.actionLeave, "leave", false, "Remove a node from builder instead of changing it")
	flags.BoolVar(&options.use, "use", false, "Set the current builder instance")

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}
