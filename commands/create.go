package commands

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/driver"
	k8sutil "github.com/docker/buildx/driver/kubernetes/util"
	remoteutil "github.com/docker/buildx/driver/remote/util"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/dockerutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	dopts "github.com/docker/cli/opts"
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
	bootstrap    bool
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

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	// Ensure the file lock gets released no matter what happens.
	defer release()

	name := in.name
	if name == "" {
		name, err = store.GenerateName(txn)
		if err != nil {
			return err
		}
	}

	if !in.actionLeave && !in.actionAppend {
		contexts, err := dockerCli.ContextStore().List()
		if err != nil {
			return err
		}
		for _, c := range contexts {
			if c.Name == name {
				logrus.Warnf("instance name %q already exists as context builder", name)
				break
			}
		}
	}

	ng, err := txn.NodeGroupByName(name)
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			if in.actionAppend && in.name != "" {
				logrus.Warnf("failed to find %q for append, creating a new instance instead", in.name)
			}
			if in.actionLeave {
				return errors.Errorf("failed to find instance %q for leave", in.name)
			}
		} else {
			return err
		}
	}

	buildkitHost := os.Getenv("BUILDKIT_HOST")

	driverName := in.driver
	if driverName == "" {
		if ng != nil {
			driverName = ng.Driver
		} else if len(args) == 0 && buildkitHost != "" {
			driverName = "remote"
		} else {
			var arg string
			if len(args) > 0 {
				arg = args[0]
			}
			f, err := driver.GetDefaultFactory(ctx, arg, dockerCli.Client(), true, nil)
			if err != nil {
				return err
			}
			if f == nil {
				return errors.Errorf("no valid drivers found")
			}
			driverName = f.Name()
		}
	}

	if ng != nil {
		if in.nodeName == "" && !in.actionAppend {
			return errors.Errorf("existing instance for %q but no append mode, specify --node to make changes for existing instances", name)
		}
		if driverName != ng.Driver {
			return errors.Errorf("existing instance for %q but has mismatched driver %q", name, ng.Driver)
		}
	}

	if _, err := driver.GetFactory(driverName, true); err != nil {
		return err
	}

	ngOriginal := ng
	if ngOriginal != nil {
		ngOriginal = ngOriginal.Copy()
	}

	if ng == nil {
		ng = &store.NodeGroup{
			Name:   name,
			Driver: driverName,
		}
	}

	var flags []string
	if in.flags != "" {
		flags, err = shlex.Split(in.flags)
		if err != nil {
			return errors.Wrap(err, "failed to parse buildkit flags")
		}
	}

	var ep string
	var setEp bool
	if in.actionLeave {
		if err := ng.Leave(in.nodeName); err != nil {
			return err
		}
		ls, err := localstate.New(confutil.ConfigDir(dockerCli))
		if err != nil {
			return err
		}
		if err := ls.RemoveBuilderNode(ng.Name, in.nodeName); err != nil {
			return err
		}
	} else {
		switch {
		case driverName == "kubernetes":
			if len(args) > 0 {
				logrus.Warnf("kubernetes driver does not support endpoint args %q", args[0])
			}
			// generate node name if not provided to avoid duplicated endpoint
			// error: https://github.com/docker/setup-buildx-action/issues/215
			nodeName := in.nodeName
			if nodeName == "" {
				nodeName, err = k8sutil.GenerateNodeName(name, txn)
				if err != nil {
					return err
				}
			}
			// naming endpoint to make --append works
			ep = (&url.URL{
				Scheme: driverName,
				Path:   "/" + name,
				RawQuery: (&url.Values{
					"deployment": {nodeName},
					"kubeconfig": {os.Getenv("KUBECONFIG")},
				}).Encode(),
			}).String()
			setEp = false
		case driverName == "remote":
			if len(args) > 0 {
				ep = args[0]
			} else if buildkitHost != "" {
				ep = buildkitHost
			} else {
				return errors.Errorf("no remote endpoint provided")
			}
			ep, err = validateBuildkitEndpoint(ep)
			if err != nil {
				return err
			}
			setEp = true
		case len(args) > 0:
			ep, err = validateEndpoint(dockerCli, args[0])
			if err != nil {
				return err
			}
			setEp = true
		default:
			if dockerCli.CurrentContext() == "default" && dockerCli.DockerEndpoint().TLSData != nil {
				return errors.Errorf("could not create a builder instance with TLS data loaded from environment. Please use `docker context create <context-name>` to create a context for current environment and then create a builder instance with `docker buildx create <context-name>`")
			}
			ep, err = dockerutil.GetCurrentEndpoint(dockerCli)
			if err != nil {
				return err
			}
			setEp = false
		}

		m, err := csvToMap(in.driverOpts)
		if err != nil {
			return err
		}

		if in.configFile == "" {
			// if buildkit config is not provided, check if the default one is
			// available and use it
			if f, ok := confutil.DefaultConfigFile(dockerCli); ok {
				logrus.Warnf("Using default BuildKit config in %s", f)
				in.configFile = f
			}
		}

		if err := ng.Update(in.nodeName, ep, in.platform, setEp, in.actionAppend, flags, in.configFile, m); err != nil {
			return err
		}
	}

	if err := txn.Save(ng); err != nil {
		return err
	}

	b, err := builder.New(dockerCli,
		builder.WithName(ng.Name),
		builder.WithStore(txn),
		builder.WithSkippedValidation(),
	)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	nodes, err := b.LoadNodes(timeoutCtx, builder.WithData())
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if err := node.Err; err != nil {
			err := errors.Errorf("failed to initialize builder %s (%s): %s", ng.Name, node.Name, err)
			var err2 error
			if ngOriginal == nil {
				err2 = txn.Remove(ng.Name)
			} else {
				err2 = txn.Save(ngOriginal)
			}
			if err2 != nil {
				logrus.Warnf("Could not rollback to previous state: %s", err2)
			}
			return err
		}
	}

	if in.use && ep != "" {
		current, err := dockerutil.GetCurrentEndpoint(dockerCli)
		if err != nil {
			return err
		}
		if err := txn.SetCurrent(current, ng.Name, false, false); err != nil {
			return err
		}
	}

	// The store is no longer used from this point.
	// Release it so we aren't holding the file lock during the boot.
	release()

	if in.bootstrap {
		if _, err = b.Boot(ctx); err != nil {
			return err
		}
	}

	fmt.Printf("%s\n", ng.Name)
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
			return runCreate(dockerCli, options, args)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()

	flags.StringVar(&options.name, "name", "", "Builder instance name")
	flags.StringVar(&options.driver, "driver", "", fmt.Sprintf("Driver to use (available: %s)", drivers.String()))
	flags.StringVar(&options.nodeName, "node", "", "Create/modify node with given name")
	flags.StringVar(&options.flags, "buildkitd-flags", "", "Flags for buildkitd daemon")
	flags.StringVar(&options.configFile, "config", "", "BuildKit config file")
	flags.StringArrayVar(&options.platform, "platform", []string{}, "Fixed platforms for current node")
	flags.StringArrayVar(&options.driverOpts, "driver-opt", []string{}, "Options for the driver")
	flags.BoolVar(&options.bootstrap, "bootstrap", false, "Boot builder after creation")

	flags.BoolVar(&options.actionAppend, "append", false, "Append a node to builder instead of changing it")
	flags.BoolVar(&options.actionLeave, "leave", false, "Remove a node from builder instead of changing it")
	flags.BoolVar(&options.use, "use", false, "Set the current builder instance")

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}

func csvToMap(in []string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
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

// validateEndpoint validates that endpoint is either a context or a docker host
func validateEndpoint(dockerCli command.Cli, ep string) (string, error) {
	dem, err := dockerutil.GetDockerEndpoint(dockerCli, ep)
	if err == nil && dem != nil {
		if ep == "default" {
			return dem.Host, nil
		}
		return ep, nil
	}
	h, err := dopts.ParseHost(true, ep)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse endpoint %s", ep)
	}
	return h, nil
}

// validateBuildkitEndpoint validates that endpoint is a valid buildkit host
func validateBuildkitEndpoint(ep string) (string, error) {
	if err := remoteutil.IsValidEndpoint(ep); err != nil {
		return "", err
	}
	return ep, nil
}
