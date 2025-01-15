package history

import (
	"context"
	"io"
	"slices"

	"github.com/containerd/containerd/v2/core/content/proxy"
	"github.com/containerd/platforms"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli/command"
	intoto "github.com/in-toto/in-toto-golang/in_toto"
	slsa02 "github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type attachmentOptions struct {
	builder  string
	typ      string
	platform string
	ref      string
	digest   digest.Digest
}

func runAttachment(ctx context.Context, dockerCli command.Cli, opts attachmentOptions) error {
	b, err := builder.New(dockerCli, builder.WithName(opts.builder))
	if err != nil {
		return err
	}

	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Err != nil {
			return node.Err
		}
	}

	recs, err := queryRecords(ctx, opts.ref, nodes)
	if err != nil {
		return err
	}

	if len(recs) == 0 {
		if opts.ref == "" {
			return errors.New("no records found")
		}
		return errors.Errorf("no record found for ref %q", opts.ref)
	}

	if opts.ref == "" {
		slices.SortFunc(recs, func(a, b historyRecord) int {
			return b.CreatedAt.AsTime().Compare(a.CreatedAt.AsTime())
		})
	}

	rec := &recs[0]

	c, err := rec.node.Driver.Client(ctx)
	if err != nil {
		return err
	}

	store := proxy.NewContentStore(c.ContentClient())

	if opts.digest != "" {
		ra, err := store.ReaderAt(ctx, ocispecs.Descriptor{Digest: opts.digest})
		if err != nil {
			return err
		}
		_, err = io.Copy(dockerCli.Out(), io.NewSectionReader(ra, 0, ra.Size()))
		return err
	}

	attachments, err := allAttachments(ctx, store, *rec)
	if err != nil {
		return err
	}

	typ := opts.typ
	switch typ {
	case "index":
		typ = ocispecs.MediaTypeImageIndex
	case "manifest":
		typ = ocispecs.MediaTypeImageManifest
	case "image":
		typ = ocispecs.MediaTypeImageConfig
	case "provenance":
		typ = slsa02.PredicateSLSAProvenance
	case "sbom":
		typ = intoto.PredicateSPDX
	}

	for _, a := range attachments {
		if opts.platform != "" && (a.platform == nil || platforms.FormatAll(*a.platform) != opts.platform) {
			continue
		}
		if typ != "" && descrType(a.descr) != typ {
			continue
		}
		ra, err := store.ReaderAt(ctx, a.descr)
		if err != nil {
			return err
		}
		_, err = io.Copy(dockerCli.Out(), io.NewSectionReader(ra, 0, ra.Size()))
		return err
	}

	return errors.Errorf("no matching attachment found for ref %q", opts.ref)
}

func attachmentCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var options attachmentOptions

	cmd := &cobra.Command{
		Use:   "attachment [OPTIONS] REF [DIGEST]",
		Short: "Inspect a build attachment",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.ref = args[0]
			}
			if len(args) > 1 {
				dgst, err := digest.Parse(args[1])
				if err != nil {
					return errors.Wrapf(err, "invalid digest %q", args[1])
				}
				options.digest = dgst
			}

			if options.digest == "" && options.platform == "" && options.typ == "" {
				return errors.New("at least one of --type, --platform or DIGEST must be specified")
			}

			options.builder = *rootOpts.Builder
			return runAttachment(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completion.Disable,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.typ, "type", "", "Type of attachment")
	flags.StringVar(&options.platform, "platform", "", "Platform of attachment")

	return cmd
}
