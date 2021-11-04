package commands

import (
	"fmt"
	"os"

	"github.com/containerd/containerd/images"
	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/appcontext"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
)

type inspectOptions struct {
	raw     bool
	builder string
}

func runInspect(dockerCli command.Cli, in inspectOptions, name string) error {
	ctx := appcontext.Context()

	txn, release, err := storeutil.GetStore(dockerCli)
	if err != nil {
		return err
	}
	defer release()

	var ng *store.NodeGroup

	if in.builder != "" {
		ng, err = storeutil.GetNodeGroup(txn, dockerCli, in.builder)
		if err != nil {
			return err
		}
	} else {
		ng, err = storeutil.GetCurrentInstance(txn, dockerCli)
		if err != nil {
			return err
		}
	}

	imageopt, err := storeutil.GetImageConfig(dockerCli, ng)
	if err != nil {
		return err
	}
	r := imagetools.New(imageopt)

	dt, desc, err := r.Get(ctx, name)
	if err != nil {
		return err
	}

	if in.raw {
		fmt.Printf("%s", dt) // avoid newline to keep digest
		return nil
	}

	switch desc.MediaType {
	// case images.MediaTypeDockerSchema2Manifest, specs.MediaTypeImageManifest:
	// TODO: handle distribution manifest and schema1
	case images.MediaTypeDockerSchema2ManifestList, ocispec.MediaTypeImageIndex:
		return imagetools.PrintManifestList(dt, desc, name, os.Stdout)
	default:
		fmt.Printf("%s\n", dt)
	}

	return nil
}

func inspectCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options inspectOptions

	cmd := &cobra.Command{
		Use:   "inspect [OPTIONS] NAME",
		Short: "Show details of image in the registry",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = rootOpts.Builder
			return runInspect(dockerCli, options, args[0])
		},
	}

	flags := cmd.Flags()

	flags.BoolVar(&options.raw, "raw", false, "Show original JSON manifest")

	return cmd
}
