package commands

import (
	"context"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli-docs-tool/annotation"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	builder string
	format  string
	raw     bool
}

func runInspect(ctx context.Context, dockerCli command.Cli, in inspectOptions, name string) error {
	if in.format != "" && in.raw {
		return errors.Errorf("format and raw cannot be used together")
	}

	b, err := builder.New(dockerCli, builder.WithName(in.builder))
	if err != nil {
		return err
	}
	imageopt, err := b.ImageOpt()
	if err != nil {
		return err
	}

	p, err := imagetools.NewPrinter(ctx, imageopt, name, in.format)
	if err != nil {
		return err
	}

	return p.Print(in.raw, dockerCli.Out())
}

func inspectCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] NAME",
		Short: "Show details of an image in the registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = *rootOpts.Builder
			return runInspect(cmd.Context(), dockerCli, options, args[0])
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()

	flags.StringVar(&options.format, "format", "", "Format the output using the given Go template")
	flags.SetAnnotation("format", annotation.DefaultValue, []string{`"{{.Manifest}}"`})

	flags.BoolVar(&options.raw, "raw", false, "Show original, unformatted JSON manifest")

	return cmd
}
