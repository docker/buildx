package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	"github.com/pkg/errors"
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
	format  string
	noTrunc bool
}

func runLs(ctx context.Context, dockerCli command.Cli, in lsOptions) error {
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

	timeoutCtx, cancel := context.WithCancelCause(ctx)
	timeoutCtx, _ = context.WithTimeoutCause(timeoutCtx, 20*time.Second, errors.WithStack(context.DeadlineExceeded)) //nolint:govet,lostcancel // no need to manually cancel this context as we already rely on parent
	defer func() { cancel(errors.WithStack(context.Canceled)) }()

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

	if hasErrors, err := lsPrint(dockerCli, current, builders, in); err != nil {
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
			return runLs(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.format, "format", formatter.TableFormatKey, "Format the output")
	flags.BoolVar(&options.noTrunc, "no-trunc", false, "Don't truncate output")

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	return cmd
}

func lsPrint(dockerCli command.Cli, current *store.NodeGroup, builders []*builder.Builder, in lsOptions) (hasErrors bool, _ error) {
	if in.format == formatter.TableFormatKey {
		in.format = lsDefaultTableFormat
	}

	ctx := formatter.Context{
		Output: dockerCli.Out(),
		Format: formatter.Format(in.format),
		Trunc:  !in.noTrunc,
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
				format: ctx.Format,
				trunc:  ctx.Trunc,
				Builder: &lsBuilder{
					Builder: b,
					Current: b.Name == current.Name,
				},
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
					trunc:  ctx.Trunc,
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
	trunc  bool
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
	pfs := platformutil.FormatInGroups(c.node.Node.Platforms, c.node.Platforms)
	if c.trunc && c.format.IsTable() {
		return truncPlatforms(pfs, 4).String()
	}
	return strings.Join(pfs, ", ")
}

func (c *lsContext) Error() string {
	if c.node.Name != "" && c.node.Err != nil {
		return c.node.Err.Error()
	} else if err := c.Builder.Err(); err != nil {
		return err.Error()
	}
	return ""
}

var truncMajorPlatforms = []string{
	"linux/amd64",
	"linux/arm64",
	"linux/arm",
	"linux/ppc64le",
	"linux/s390x",
	"linux/riscv64",
	"linux/mips64",
}

type truncatedPlatforms struct {
	res   map[string][]string
	input []string
	max   int
}

func (tp truncatedPlatforms) List() map[string][]string {
	return tp.res
}

func (tp truncatedPlatforms) String() string {
	var out []string
	var count int

	var keys []string
	for k := range tp.res {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	seen := make(map[string]struct{})
	for _, mpf := range truncMajorPlatforms {
		if tpf, ok := tp.res[mpf]; ok {
			seen[mpf] = struct{}{}
			if len(tpf) == 1 {
				out = append(out, tpf[0])
				count++
			} else {
				hasPreferredPlatform := false
				for _, pf := range tpf {
					if strings.HasSuffix(pf, "*") {
						hasPreferredPlatform = true
						break
					}
				}
				mainpf := mpf
				if hasPreferredPlatform {
					mainpf += "*"
				}
				out = append(out, fmt.Sprintf("%s (+%d)", mainpf, len(tpf)))
				count += len(tpf)
			}
		}
	}

	for _, mpf := range keys {
		if len(out) >= tp.max {
			break
		}
		if _, ok := seen[mpf]; ok {
			continue
		}
		if len(tp.res[mpf]) == 1 {
			out = append(out, tp.res[mpf][0])
			count++
		} else {
			hasPreferredPlatform := false
			for _, pf := range tp.res[mpf] {
				if strings.HasSuffix(pf, "*") {
					hasPreferredPlatform = true
					break
				}
			}
			mainpf := mpf
			if hasPreferredPlatform {
				mainpf += "*"
			}
			out = append(out, fmt.Sprintf("%s (+%d)", mainpf, len(tp.res[mpf])))
			count += len(tp.res[mpf])
		}
	}

	left := len(tp.input) - count
	if left > 0 {
		out = append(out, fmt.Sprintf("(%d more)", left))
	}

	return strings.Join(out, ", ")
}

func truncPlatforms(pfs []string, max int) truncatedPlatforms {
	res := make(map[string][]string)
	for _, mpf := range truncMajorPlatforms {
		for _, pf := range pfs {
			if len(res) >= max {
				break
			}
			pp, err := platforms.Parse(strings.TrimSuffix(pf, "*"))
			if err != nil {
				continue
			}
			if pp.OS+"/"+pp.Architecture == mpf {
				res[mpf] = append(res[mpf], pf)
			}
		}
	}
	left := make(map[string][]string)
	for _, pf := range pfs {
		if len(res) >= max {
			break
		}
		pp, err := platforms.Parse(strings.TrimSuffix(pf, "*"))
		if err != nil {
			continue
		}
		ppf := strings.TrimSuffix(pp.OS+"/"+pp.Architecture, "*")
		if _, ok := res[ppf]; !ok {
			left[ppf] = append(left[ppf], pf)
		}
	}
	for k, v := range left {
		res[k] = v
	}
	return truncatedPlatforms{
		res:   res,
		input: pfs,
		max:   max,
	}
}
