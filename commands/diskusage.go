package commands

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/cli/opts"
	"github.com/docker/go-units"
	"github.com/moby/buildkit/client"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const (
	duIDHeader          = "ID"
	duParentsHeader     = "PARENTS"
	duCreatedAtHeader   = "CREATED AT"
	duMutableHeader     = "MUTABLE"
	duReclaimHeader     = "RECLAIMABLE"
	duSharedHeader      = "SHARED"
	duSizeHeader        = "SIZE"
	duDescriptionHeader = "DESCRIPTION"
	duUsageHeader       = "USAGE COUNT"
	duLastUsedAtHeader  = "LAST ACCESSED"
	duTypeHeader        = "TYPE"

	duDefaultTableFormat = "table {{.ID}}\t{{.Reclaimable}}\t{{.Size}}\t{{.LastUsedAt}}"

	duDefaultPrettyTemplate = `ID:           {{.ID}}
{{- if .Parents }}
Parents:
{{- range .Parents }}
 - {{.}}
{{- end }}
{{- end }}
Created at:   {{.CreatedAt}}
Mutable:      {{.Mutable}}
Reclaimable:  {{.Reclaimable}}
Shared:       {{.Shared}}
Size:         {{.Size}}
{{- if .Description}}
Description:  {{ .Description }}
{{- end }}
Usage count:  {{.UsageCount}}
{{- if .LastUsedAt}}
Last used:    {{ .LastUsedAt }}
{{- end }}
{{- if .Type}}
Type:         {{ .Type }}
{{- end }}
`
)

type duOptions struct {
	builder string
	filter  opts.FilterOpt
	verbose bool
	format  string
	timeout time.Duration
}

func runDiskUsage(ctx context.Context, dockerCli command.Cli, opts duOptions) error {
	if opts.format != "" && opts.verbose {
		return errors.New("--format and --verbose cannot be used together")
	} else if opts.format == "" {
		if opts.verbose {
			opts.format = duDefaultPrettyTemplate
		} else {
			opts.format = duDefaultTableFormat
		}
	} else if opts.format == formatter.PrettyFormatKey {
		opts.format = duDefaultPrettyTemplate
	} else if opts.format == formatter.TableFormatKey {
		opts.format = duDefaultTableFormat
	}

	pi, err := toBuildkitPruneInfo(opts.filter.Value())
	if err != nil {
		return err
	}

	b, err := builder.New(dockerCli, builder.WithName(opts.builder))
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithCancelCause(ctx)
	timeoutCtx, _ = context.WithTimeoutCause(timeoutCtx, opts.timeout, errors.WithStack(context.DeadlineExceeded)) //nolint:govet // no need to manually cancel this context as we already rely on parent
	defer func() { cancel(errors.WithStack(context.Canceled)) }()

	nodes, err := b.LoadNodes(timeoutCtx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Err != nil {
			return node.Err
		}
	}

	out := make([][]*client.UsageInfo, len(nodes))

	eg, ctx := errgroup.WithContext(ctx)
	for i, node := range nodes {
		func(i int, node builder.Node) {
			eg.Go(func() error {
				if node.Driver != nil {
					c, err := node.Driver.Client(ctx)
					if err != nil {
						return err
					}
					du, err := c.DiskUsage(ctx, client.WithFilter(pi.Filter))
					if err != nil {
						return err
					}
					out[i] = du
					return nil
				}
				return nil
			})
		}(i, node)
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	fctx := formatter.Context{
		Output: dockerCli.Out(),
		Format: formatter.Format(opts.format),
	}

	var dus []*client.UsageInfo
	for _, du := range out {
		if du != nil {
			dus = append(dus, du...)
		}
	}

	render := func(format func(subContext formatter.SubContext) error) error {
		for _, du := range dus {
			if err := format(&diskusageContext{
				format: fctx.Format,
				du:     du,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	duCtx := diskusageContext{}
	duCtx.Header = formatter.SubHeaderContext{
		"ID":          duIDHeader,
		"Parents":     duParentsHeader,
		"CreatedAt":   duCreatedAtHeader,
		"Mutable":     duMutableHeader,
		"Reclaimable": duReclaimHeader,
		"Shared":      duSharedHeader,
		"Size":        duSizeHeader,
		"Description": duDescriptionHeader,
		"UsageCount":  duUsageHeader,
		"LastUsedAt":  duLastUsedAtHeader,
		"Type":        duTypeHeader,
	}

	defer func() {
		if (fctx.Format != duDefaultTableFormat && fctx.Format != duDefaultPrettyTemplate) || fctx.Format.IsJSON() || len(opts.filter.Value()) > 0 {
			return
		}
		printSummary(dockerCli.Out(), out)
	}()

	return fctx.Write(&duCtx, render)
}

func duCmd(dockerCli command.Cli, rootOpts *rootOptions) *cobra.Command {
	options := duOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "du",
		Short: "Disk usage",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.builder
			options.timeout = rootOpts.timeout
			return runDiskUsage(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flags.Var(&options.filter, "filter", "Provide filter values")
	flags.BoolVar(&options.verbose, "verbose", false, `Shorthand for "--format=pretty"`)
	flags.StringVar(&options.format, "format", "", "Format the output")

	return cmd
}

type diskusageContext struct {
	formatter.HeaderContext
	format formatter.Format
	du     *client.UsageInfo
}

func (d *diskusageContext) MarshalJSON() ([]byte, error) {
	return formatter.MarshalJSON(d)
}

func (d *diskusageContext) ID() string {
	id := d.du.ID
	if d.format.IsTable() && d.du.Mutable {
		id += "*"
	}
	return id
}

func (d *diskusageContext) Parents() []string {
	return d.du.Parents
}

func (d *diskusageContext) CreatedAt() string {
	return d.du.CreatedAt.String()
}

func (d *diskusageContext) Mutable() bool {
	return d.du.Mutable
}

func (d *diskusageContext) Reclaimable() bool {
	return !d.du.InUse
}

func (d *diskusageContext) Shared() bool {
	return d.du.Shared
}

func (d *diskusageContext) Size() string {
	size := units.HumanSize(float64(d.du.Size))
	if d.format.IsTable() && d.du.Shared {
		size += "*"
	}
	return size
}

func (d *diskusageContext) Description() string {
	return d.du.Description
}

func (d *diskusageContext) UsageCount() int {
	return d.du.UsageCount
}

func (d *diskusageContext) LastUsedAt() string {
	if d.du.LastUsedAt != nil {
		return units.HumanDuration(time.Since(*d.du.LastUsedAt)) + " ago"
	}
	return ""
}

func (d *diskusageContext) Type() string {
	return string(d.du.RecordType)
}

func printSummary(w io.Writer, dus [][]*client.UsageInfo) {
	total := int64(0)
	reclaimable := int64(0)
	shared := int64(0)

	for _, du := range dus {
		for _, di := range du {
			if di.Size > 0 {
				total += di.Size
				if !di.InUse {
					reclaimable += di.Size
				}
			}
			if di.Shared {
				shared += di.Size
			}
		}
	}

	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	if shared > 0 {
		fmt.Fprintf(tw, "Shared:\t%s\n", units.HumanSize(float64(shared)))
		fmt.Fprintf(tw, "Private:\t%s\n", units.HumanSize(float64(total-shared)))
	}
	fmt.Fprintf(tw, "Reclaimable:\t%s\n", units.HumanSize(float64(reclaimable)))
	fmt.Fprintf(tw, "Total:\t%s\n", units.HumanSize(float64(total)))
	tw.Flush()
}
