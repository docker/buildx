package commands

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/google/shlex"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type createOptions struct {
	name         string
	driver       string
	nodeName     string
	platform     []string
	actionAppend bool
	actionLeave  bool
	use          bool
	flags        string
	configFile   string
	driverOpts   []string
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

	var flags []string
	if in.flags != "" {
		flags, err = shlex.Split(in.flags)
		if err != nil {
			return errors.Wrap(err, "failed to parse buildkit flags")
		}
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
			if dockerCli.CurrentContext() == "default" && dockerCli.DockerEndpoint().TLSData != nil {
				return errors.Errorf("could not create a builder instance with TLS data loaded from environment. Please use `docker context create <context-name>` to create a context for current environment and then create a builder instance with `docker buildx create <context-name>`")
			}

			ep, err = getCurrentEndpoint(dockerCli)
			if err != nil {
				return err
			}
		}
		m, err := csvToMap(in.driverOpts)
		if err != nil {
			return err
		}
		if err := ng.Update(in.nodeName, ep, in.platform, len(args) > 0, in.actionAppend, flags, in.configFile, m); err != nil {
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

	var drivers []string
	for s := range driver.GetFactories() {
		drivers = append(drivers, s)
	}

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
	flags.StringVar(&options.driver, "driver", "", fmt.Sprintf("Driver to use (available: %v)", drivers))
	flags.StringVar(&options.nodeName, "node", "", "Create/modify node with given name")
	flags.StringVar(&options.flags, "buildkitd-flags", "", "Flags for buildkitd daemon")
	flags.StringVar(&options.configFile, "config", "", "BuildKit config file")
	flags.StringArrayVar(&options.platform, "platform", []string{}, "Fixed platforms for current node")
	flags.StringArrayVar(&options.driverOpts, "driver-opt", []string{}, "Options for the driver")

	flags.BoolVar(&options.actionAppend, "append", false, "Append a node to builder instead of changing it")
	flags.BoolVar(&options.actionLeave, "leave", false, "Remove a node from builder instead of changing it")
	flags.BoolVar(&options.use, "use", false, "Set the current builder instance")

	_ = flags

	return cmd
}

func csvToMap(in []string) (map[string]string, error) {
	m := make(map[string]string, len(in))
	for _, s := range in {
		csvReader := csv.NewReader(strings.NewReader(s))
		fields, err := csvReader.Read()
		if err != nil {
			return nil, err
		}
		for _, v := range fields {
			p := strings.SplitN(v, "=", 2)
			if len(p) != 2 {
				return nil, errors.Errorf("invalid value %q, expecting k=v", v)
			}
			m[p[0]] = p[1]
		}
	}
	return m, nil
}
