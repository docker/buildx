package commands

import (
	"github.com/docker/buildx/builder"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/formatter"
	cliflags "github.com/docker/cli/cli/flags"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	builderName string
	format      string
	refs        []string
}

func inspectCmd(dockerCLI command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] IMAGE [IMAGE...]",
		Short: "Show detailed information on one or more images in the registry",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builderName = *rootOpts.Builder
			opts.refs = args
			return runInspect(dockerCLI, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&opts.format, "format", "f", "", cliflags.InspectFormatHelp)

	return cmd
}

func runInspect(dockerCli command.Cli, opts inspectOptions, name string) error {
	b, err := builder.New(dockerCli, builder.WithName(opts.builderName))
	if err != nil {
		return err
	}

	imageopt, err := b.ImageOpt()
	if err != nil {
		return err
	}

	return inspectFormatWrite(formatter.Context{
		Output: dockerCli.Out(),
		Format: makeFormat(opts.format),
	}, name, imageopt)
}

// func runInspect(dockerCLI command.Cli, opts inspectOptions, name string) error {
// 	b, err := builder.New(dockerCLI, builder.WithName(opts.builderName))
// 	if err != nil {
// 		return fmt.Errorf("new builder: %w", err)
// 	}

// 	imgopt, err := b.ImageOpt()
// 	if err != nil {
// 		return fmt.Errorf("image opt: %w", err)
// 	}

// 	resolver := imagetools.New(imgopt)
// 	ctx := appcontext.Context()
// 	return inspect.Inspect(dockerCLI.Out(), opts.refs, opts.format, func(ref string) (interface{}, []byte, error) {
// 		newref, err := imagetools.ParseRef(ref)
// 		if err != nil {
// 			return nil, nil, err
// 		}

// 		dt, mfst, err := resolver.Get(ctx, newref.String())
// 		if err != nil {
// 			return nil, nil, err
// 		}
// 		return mfst, dt, err
// 	})
// }
