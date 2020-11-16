package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/build"
	"github.com/docker/buildx/driver"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type inspectOptions struct {
	bootstrap bool
	builder   string
}

type dinfo struct {
	di        *build.DriverInfo
	info      *driver.Info
	platforms []specs.Platform
	err       error
}

type nginfo struct {
	ng      *store.NodeGroup
	drivers []dinfo
	err     error
}

func runInspect(dockerCli command.Cli, in inspectOptions) error {
	ctx := appcontext.Context()

	txn, release, err := getStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	var ng *store.NodeGroup

	if in.builder != "" {
		ng, err = getNodeGroup(txn, dockerCli, in.builder)
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

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	err = loadNodeGroupData(timeoutCtx, dockerCli, ngi)

	var bootNgi *nginfo
	if in.bootstrap {
		var ok bool
		ok, err = boot(ctx, ngi, dockerCli)
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

func boot(ctx context.Context, ngi *nginfo, dockerCli command.Cli) (bool, error) {
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
		return false, nil
	}

	pw := progress.NewPrinter(context.TODO(), os.Stderr, "auto")

	mw := progress.NewMultiWriter(pw)

	eg, _ := errgroup.WithContext(ctx)
	for _, idx := range toBoot {
		func(idx int) {
			eg.Go(func() error {
				pw := mw.WithPrefix(ngi.ng.Nodes[idx].Name, len(toBoot) > 1)
				_, err := driver.Boot(ctx, ngi.drivers[idx].di.Driver, dockerCli.ConfigFile(), pw)
				if err != nil {
					ngi.drivers[idx].err = err
				}
				close(pw.Status())
				<-pw.Done()
				return nil
			})
		}(idx)
	}

	return true, eg.Wait()
}
