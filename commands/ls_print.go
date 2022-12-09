package commands

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/util/platformutil"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
)

const (
	lsNameNodeHeader       = "NAME/NODE"
	lsDriverEndpointHeader = "DRIVER/ENDPOINT"
	lsStatusHeader         = "STATUS"
	lsLastActivityHeader   = "LAST ACTIVITY"
	lsBuildkitHeader       = "BUILDKIT"
	lsPlatformsHeader      = "PLATFORMS"

	lsMaxPlatforms = 6
	lsIndent       = ` \_ `

	lsDefaultTableFormat = "table {{.NameNode}}\t{{.DriverEndpoint}}\t{{.Status}}\t{{.Buildkit}}\t{{.Platforms}}"
	lsDefaultRawFormat   = `{{- if not .IsNode -}}
name: {{.NameNode}}
{{- if .DriverEndpoint }}
driver: {{.DriverEndpoint}}
{{- end }}
{{- if .LastActivity }}
last_activity: {{.LastActivity}}
{{- end }}
{{- if .Error }}
error: {{.Error}}
{{- else }}
nodes:
{{- end }}
{{- else }}  name: {{.NameNode}}
  endpoint: {{.DriverEndpoint}}
  status: {{.Status}}
  {{- if .Buildkit }}
  buildkit: {{.Buildkit}}
  {{- end }}
  {{- if .Platforms }}
  platforms: {{.Platforms}}
  {{- end }}
  {{- if .Error }}
  error: {{.Error}}
  {{- end }}
{{- end }}
`
)

func lsPrint(dockerCli command.Cli, current *store.NodeGroup, builders []*builder.Builder, trunc, quiet bool, format string) (bool, error) {
	lsCtx := formatter.Context{
		Output: dockerCli.Out(),
		Format: lsFormat(format, quiet),
		Trunc:  trunc,
	}
	return lsFormatWrite(lsCtx, current, builders, quiet)
}

func lsFormat(source string, quiet bool) formatter.Format {
	if quiet {
		return `{{.NameNode}}`
	}
	switch source {
	case formatter.TableFormatKey:
		return lsDefaultTableFormat
	case formatter.RawFormatKey:
		return lsDefaultRawFormat
	}
	return formatter.Format(source)
}

func lsFormatWrite(ctx formatter.Context, current *store.NodeGroup, builders []*builder.Builder, quiet bool) (hasErrors bool, _ error) {
	render := func(format func(subContext formatter.SubContext) error) error {
		for _, b := range builders {
			if err := format(&lsContext{
				format:  ctx.Format,
				trunc:   ctx.Trunc,
				quiet:   quiet,
				builder: b,
				current: current,
			}); err != nil {
				return err
			}
			if b.Err() != nil {
				if ctx.Format.IsTable() {
					hasErrors = true
				}
				continue
			}
			if ctx.Format.IsJSON() {
				continue
			}
			for _, n := range b.Nodes() {
				if n.Err != nil && ctx.Format.IsTable() {
					hasErrors = true
				}
				if err := format(&lsContext{
					format:  ctx.Format,
					trunc:   ctx.Trunc,
					quiet:   quiet,
					builder: b,
					node:    &n,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	}

	lsCtx := lsContext{}
	lsCtx.Header = formatter.SubHeaderContext{
		"NameNode":       lsNameNodeHeader,
		"DriverEndpoint": lsDriverEndpointHeader,
		"LastActivity":   lsLastActivityHeader,
		"Status":         lsStatusHeader,
		"Buildkit":       lsBuildkitHeader,
		"Platforms":      lsPlatformsHeader,
	}

	return hasErrors, ctx.Write(&lsCtx, render)
}

type lsContext struct {
	formatter.HeaderContext
	format  formatter.Format
	trunc   bool
	quiet   bool
	builder *builder.Builder
	current *store.NodeGroup
	node    *builder.Node
}

func (c *lsContext) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.builder)
}

func (c *lsContext) IsNode() bool {
	return c.node != nil
}

func (c *lsContext) IsCurrent() bool {
	return c.builder != nil && c.current.Name == c.builder.Name
}

func (c *lsContext) NameNode() string {
	if !c.IsNode() {
		name := c.builder.Name
		if !c.quiet && c.current.Name == name {
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
	if !c.IsNode() {
		return c.builder.Driver
	}
	if c.format.IsTable() {
		return lsIndent + c.node.Endpoint
	}
	return c.node.Endpoint
}

func (c *lsContext) LastActivity() string {
	if c.IsNode() || c.builder.LastActivity.IsZero() {
		return ""
	}
	return c.builder.LastActivity.UTC().Format(time.RFC3339)
}

func (c *lsContext) Status() string {
	if !c.IsNode() {
		if c.builder.Err() != nil {
			return "error"
		}
		return ""
	}
	var status string
	if c.node.Err != nil {
		status = "error"
	}
	if c.node.DriverInfo != nil {
		status = c.node.DriverInfo.Status.String()
	}
	return status
}

func (c *lsContext) Buildkit() string {
	if !c.IsNode() {
		return ""
	}
	return c.node.Version
}

func (c *lsContext) Platforms() string {
	if !c.IsNode() {
		return ""
	}
	platforms := platformutil.FormatInGroups(c.node.Node.Platforms, c.node.Platforms)
	if c.trunc && c.format.IsTable() && len(platforms) > lsMaxPlatforms {
		return strings.Join(platforms[0:lsMaxPlatforms], ", ") + ",…"
	}
	return strings.Join(platforms, ", ")
}

func (c *lsContext) Error() string {
	var err error
	if c.IsNode() {
		err = c.node.Err
	} else {
		err = c.builder.Err()
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
