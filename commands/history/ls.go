package history

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"slices"
	"time"

	"github.com/containerd/console"
	"github.com/docker/buildx/localstate"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/desktop"
	"github.com/docker/buildx/util/gitutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/docker/go-units"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	lsHeaderBuildID  = "BUILD ID"
	lsHeaderName     = "NAME"
	lsHeaderStatus   = "STATUS"
	lsHeaderCreated  = "CREATED AT"
	lsHeaderDuration = "DURATION"
	lsHeaderLink     = ""

	lsDefaultTableFormat = "table {{.Ref}}\t{{.Name}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Duration}}\t{{.Link}}"

	headerKeyTimestamp = "buildkit-current-timestamp"
)

type lsOptions struct {
	builder string
	format  string
	noTrunc bool

	filters []string
	local   bool
}

func runLs(ctx context.Context, dockerCli command.Cli, opts lsOptions) error {
	nodes, err := loadNodes(ctx, dockerCli, opts.builder)
	if err != nil {
		return err
	}

	queryOptions := &queryOptions{}

	if opts.local {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		gitc, err := gitutil.New(gitutil.WithContext(ctx), gitutil.WithWorkingDir(wd))
		if err != nil {
			if st, err1 := os.Stat(path.Join(wd, ".git")); err1 == nil && st.IsDir() {
				return errors.Wrap(err, "git was not found in the system")
			}
			return errors.Wrapf(err, "could not find git repository for local filter")
		}
		remote, err := gitc.RemoteURL()
		if err != nil {
			return errors.Wrapf(err, "could not get remote URL for local filter")
		}
		queryOptions.Filters = append(queryOptions.Filters, fmt.Sprintf("repository=%s", remote))
	}
	queryOptions.Filters = append(queryOptions.Filters, opts.filters...)

	out, err := queryRecords(ctx, "", nodes, queryOptions)
	if err != nil {
		return err
	}

	ls, err := localstate.New(confutil.NewConfig(dockerCli))
	if err != nil {
		return err
	}

	for i, rec := range out {
		st, _ := ls.ReadRef(rec.node.Builder, rec.node.Name, rec.Ref)
		rec.name = BuildName(rec.FrontendAttrs, st)
		out[i] = rec
	}

	return lsPrint(dockerCli, out, opts)
}

func lsCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options lsOptions

	cmd := &cobra.Command{
		Use:   "ls [OPTIONS]",
		Short: "List build records",
		Args:  cli.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = *rootOpts.Builder
			return runLs(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.format, "format", formatter.TableFormatKey, "Format the output")
	flags.BoolVar(&options.noTrunc, "no-trunc", false, "Don't truncate output")
	flags.StringArrayVar(&options.filters, "filter", nil, `Provide filter values (e.g., "status=error")`)
	flags.BoolVar(&options.local, "local", false, "List records for current repository only")

	return cmd
}

func lsPrint(dockerCli command.Cli, records []historyRecord, in lsOptions) error {
	if in.format == formatter.TableFormatKey {
		in.format = lsDefaultTableFormat
	}

	ctx := formatter.Context{
		Output: dockerCli.Out(),
		Format: formatter.Format(in.format),
		Trunc:  !in.noTrunc,
	}

	slices.SortFunc(records, func(a, b historyRecord) int {
		if a.CompletedAt == nil && b.CompletedAt != nil {
			return -1
		}
		if a.CompletedAt != nil && b.CompletedAt == nil {
			return 1
		}
		return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
	})

	var term bool
	if _, err := console.ConsoleFromFile(os.Stdout); err == nil {
		term = true
	}
	render := func(format func(subContext formatter.SubContext) error) error {
		for _, r := range records {
			if err := format(&lsContext{
				format: formatter.Format(in.format),
				isTerm: term,
				trunc:  !in.noTrunc,
				record: &r,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	lsCtx := lsContext{
		isTerm: term,
		trunc:  !in.noTrunc,
	}
	lsCtx.Header = formatter.SubHeaderContext{
		"Ref":       lsHeaderBuildID,
		"Name":      lsHeaderName,
		"Status":    lsHeaderStatus,
		"CreatedAt": lsHeaderCreated,
		"Duration":  lsHeaderDuration,
		"Link":      lsHeaderLink,
	}

	return ctx.Write(&lsCtx, render)
}

type lsContext struct {
	formatter.HeaderContext

	isTerm bool
	trunc  bool
	format formatter.Format
	record *historyRecord
}

func (c *lsContext) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"ref":             c.FullRef(),
		"name":            c.Name(),
		"status":          c.Status(),
		"created_at":      c.record.CreatedAt.AsTime().Format(time.RFC3339Nano),
		"total_steps":     c.record.NumTotalSteps,
		"completed_steps": c.record.NumCompletedSteps,
		"cached_steps":    c.record.NumCachedSteps,
	}
	if c.record.CompletedAt != nil {
		m["completed_at"] = c.record.CompletedAt.AsTime().Format(time.RFC3339Nano)
	}
	return json.Marshal(m)
}

func (c *lsContext) Ref() string {
	return c.record.Ref
}

func (c *lsContext) FullRef() string {
	return fmt.Sprintf("%s/%s/%s", c.record.node.Builder, c.record.node.Name, c.record.Ref)
}

func (c *lsContext) Name() string {
	name := c.record.name
	if c.trunc && c.format.IsTable() {
		return trimBeginning(name, 36)
	}
	return name
}

func (c *lsContext) Status() string {
	if c.record.CompletedAt != nil {
		if c.record.Error != nil {
			return "Error"
		}
		return "Completed"
	}
	return "Running"
}

func (c *lsContext) CreatedAt() string {
	return units.HumanDuration(time.Since(c.record.CreatedAt.AsTime())) + " ago"
}

func (c *lsContext) Duration() string {
	lastTime := c.record.currentTimestamp
	if c.record.CompletedAt != nil {
		tm := c.record.CompletedAt.AsTime()
		lastTime = &tm
	}
	if lastTime == nil {
		return ""
	}
	v := formatDuration(lastTime.Sub(c.record.CreatedAt.AsTime()))
	if c.record.CompletedAt == nil {
		v += "+"
	}
	return v
}

func (c *lsContext) Link() string {
	url := desktop.BuildURL(c.FullRef())
	if c.format.IsTable() {
		if c.isTerm {
			return desktop.ANSIHyperlink(url, "Open")
		}
		return ""
	}
	return url
}
