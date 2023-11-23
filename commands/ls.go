package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const (
	lsNameNodeHeader       = "NAME/NODE"
	lsDriverEndpointHeader = "DRIVER/ENDPOINT"
	lsStatusHeader         = "STATUS"
	lsLastActivityHeader   = "LAST ACTIVITY"
	lsBuildkitHeader       = "BUILDKIT"
	lsPlatformsHeader      = "PLATFORMS"

	lsIndent = ` \_ `

	lsDefaultTableFormat = "table {{.Name}}\t{{.DriverEndpoint}}\t{{.Status}}\t{{.Buildkit}}\t{{.Platforms}}"
)

type lsOptions struct {
	format string
}

func runLs(dockerCli command.Cli, in lsOptions) error {
	ctx := appcontext.Context()

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	current, err := storeutil.GetCurrentInstance(txn, dockerCli)
	if err != nil {
		return err
	}

	builders, err := builder.GetBuilders(dockerCli, txn)
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	eg, _ := errgroup.WithContext(timeoutCtx)
	for _, b := range builders {
		func(b *builder.Builder) {
			eg.Go(func() error {
				_, _ = b.LoadNodes(timeoutCtx, builder.WithData())
				return nil
			})
		}(b)
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	if hasErrors, err := lsPrint(dockerCli, current, builders, in.format); err != nil {
		return err
	} else if hasErrors {
		_, _ = fmt.Fprintf(dockerCli.Err(), "\n")
		for _, b := range builders {
			if b.Err() != nil {
				_, _ = fmt.Fprintf(dockerCli.Err(), "Cannot load builder %s: %s\n", b.Name, strings.TrimSpace(b.Err().Error()))
			} else {
				for _, d := range b.Nodes() {
					if d.Err != nil {
						_, _ = fmt.Fprintf(dockerCli.Err(), "Failed to get status for %s (%s): %s\n", b.Name, d.Name, strings.TrimSpace(d.Err.Error()))
					}
				}
			}
		}
	}

	return nil
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
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.format, "format", formatter.TableFormatKey, "Format the output")

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}

func lsPrint(dockerCli command.Cli, current *store.NodeGroup, builders []*builder.Builder, format string) (hasErrors bool, _ error) {
	if format == formatter.TableFormatKey {
		format = lsDefaultTableFormat
	}

	ctx := formatter.Context{
		Output: dockerCli.Out(),
		Format: formatter.Format(format),
	}

	sort.SliceStable(builders, func(i, j int) bool {
		ierr := builders[i].Err() != nil
		jerr := builders[j].Err() != nil
		if ierr && !jerr {
			return false
		} else if !ierr && jerr {
			return true
		}
		return i < j
	})

	render := func(format func(subContext formatter.SubContext) error) error {
		for _, b := range builders {
			if err := format(&lsContext{
				Builder: &lsBuilder{
					Builder: b,
					Current: b.Name == current.Name,
				},
				format: ctx.Format,
			}); err != nil {
				return err
			}
			if b.Err() != nil {
				if ctx.Format.IsTable() {
					hasErrors = true
				}
				continue
			}
			for _, n := range b.Nodes() {
				if n.Err != nil {
					if ctx.Format.IsTable() {
						hasErrors = true
					}
				}
				if err := format(&lsContext{
					format: ctx.Format,
					Builder: &lsBuilder{
						Builder: b,
						Current: b.Name == current.Name,
					},
					node: n,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}

	lsCtx := lsContext{}
	lsCtx.Header = formatter.SubHeaderContext{
		"Name":           lsNameNodeHeader,
		"DriverEndpoint": lsDriverEndpointHeader,
		"LastActivity":   lsLastActivityHeader,
		"Status":         lsStatusHeader,
		"Buildkit":       lsBuildkitHeader,
		"Platforms":      lsPlatformsHeader,
	}

	return hasErrors, ctx.Write(&lsCtx, render)
}

type lsBuilder struct {
	*builder.Builder
	Current bool
}

type lsContext struct {
	formatter.HeaderContext
	Builder *lsBuilder

	format formatter.Format
	node   builder.Node
}

func (c *lsContext) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Builder)
}

func (c *lsContext) Name() string {
	if c.node.Name == "" {
		name := c.Builder.Name
		if c.Builder.Current && c.format.IsTable() {
			name += "*"
		}
		return name
	}
	if c.format.IsTable() {
		return lsIndent + c.node.Name
	}
	return c.node.Name
}

func (c *lsContext) DriverEndpoint() string {
	if c.node.Name == "" {
		return c.Builder.Driver
	}
	if c.format.IsTable() {
		return lsIndent + c.node.Endpoint
	}
	return c.node.Endpoint
}

func (c *lsContext) LastActivity() string {
	if c.node.Name != "" || c.Builder.LastActivity.IsZero() {
		return ""
	}
	return c.Builder.LastActivity.UTC().Format(time.RFC3339)
}

func (c *lsContext) Status() string {
	if c.node.Name == "" {
		if c.Builder.Err() != nil {
			return "error"
		}
		return ""
	}
	if c.node.Err != nil {
		return "error"
	}
	if c.node.DriverInfo != nil {
		return c.node.DriverInfo.Status.String()
	}
	return ""
}

func (c *lsContext) Buildkit() string {
	if c.node.Name == "" {
		return ""
	}
	return c.node.Version
}

func (c *lsContext) Platforms() string {
	if c.node.Name == "" {
		return ""
	}
	return strings.Join(platformutil.FormatInGroups(c.node.Node.Platforms, c.node.Platforms), ", ")
}

func (c *lsContext) Error() string {
	if c.node.Name != "" && c.node.Err != nil {
		return c.node.Err.Error()
	} else if err := c.Builder.Err(); err != nil {
		return err.Error()
	}
	return ""
}
