package commands

import (
	"fmt"
	"os"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/buildx/driver"
	"github.com/tonistiigi/buildx/store"
)

type createOptions struct {
	name         string
	driver       string
	nodeName     string
	platform     []string
	actionAppend bool
	actionLeave  bool
	use          bool
	// upgrade      bool // perform upgrade of the driver
}

func runCreate(dockerCli command.Cli, in createOptions, args []string) error {
	ctx := appcontext.Context()

	if in.name == "default" {
		return errors.Errorf("default is a reserved name and cannot be used to identify builder instance")
	}

	if in.actionLeave {
		if in.name == "" {
			return errors.Errorf("leave requires instance name")
		}
		if in.nodeName == "" {
			return errors.Errorf("leave requires node name but --node not set")
		}
	}

	if in.actionAppend {
		if in.name == "" {
			logrus.Warnf("append used without name, creating a new instance instead")
		}
	}

	driverName := in.driver
	if driverName == "" {
		f, err := driver.GetDefaultFactory(ctx, dockerCli.Client(), true)
		if err != nil {
			return err
		}
		if f == nil {
			return errors.Errorf("no valid drivers found")
		}
		driverName = f.Name()
	}

	if driver.GetFactory(driverName, true) == nil {
		return errors.Errorf("failed to find driver %q", in.driver)
	}

	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	name := in.name
	if name == "" {
		name, err = store.GenerateName(txn)
		if err != nil {
			return err
		}
	}

	ng, err := txn.NodeGroupByName(name)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			if in.actionAppend && in.name != "" {
				logrus.Warnf("failed to find %q for append, creating a new instance instead", in.name)
			}
			if in.actionLeave {
				return errors.Errorf("failed to find instance %q for leave", name)
			}
		} else {
			return err
		}
	}

	if ng != nil {
		if in.nodeName == "" && !in.actionAppend {
			return errors.Errorf("existing instance for %s but no append mode, specify --node to make changes for existing instances", name)
		}
	}

	if ng == nil {
		ng = &store.NodeGroup{
			Name: name,
		}
	}

	if ng.Driver == "" || in.driver != "" {
		ng.Driver = driverName
	}

	var ep string
	if in.actionLeave {
		if err := ng.Leave(in.nodeName); err != nil {
			return err
		}
	} else {
		if len(args) > 0 {
			ep, err = validateEndpoint(dockerCli, args[0])
			if err != nil {
				return err
			}
		} else {
			ep, err = getCurrentEndpoint(dockerCli)
			if err != nil {
				return err
			}
		}
		if err := ng.Update(in.nodeName, ep, in.platform, len(args) > 0, in.actionAppend); err != nil {
			return err
		}
	}

	if err := txn.Save(ng); err != nil {
		return err
	}

	if in.use && ep != "" {
		current, err := getCurrentEndpoint(dockerCli)
		if err != nil {
			return err
		}
		if err := txn.SetCurrent(current, ng.Name, false, false); err != nil {
			return err
		}
	}

	fmt.Printf("%s\n", ng.Name)
	return nil
}

func createCmd(dockerCli command.Cli) *cobra.Command {
	var options createOptions

	cmd := &cobra.Command{
		Use:   "create [OPTIONS] [CONTEXT|ENDPOINT]",
		Short: "Create a new builder instance",
		Args:  cli.RequiresMaxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(dockerCli, options, args)
		},
	}

	flags := cmd.Flags()

	flags.StringVar(&options.name, "name", "", "Builder instance name")
	flags.StringVar(&options.driver, "driver", "", "Driver to use (eg. docker-container)")
	flags.StringVar(&options.nodeName, "node", "", "Create/modify node with given name")
	flags.StringArrayVar(&options.platform, "platform", []string{}, "Fixed platforms for current node")

	flags.BoolVar(&options.actionAppend, "append", false, "Append a node to builder instead of changing it")
	flags.BoolVar(&options.actionLeave, "leave", false, "Remove a node from builder instead of changing it")
	flags.BoolVar(&options.use, "use", false, "Set the current builder instance")

	_ = flags

	return cmd
}
