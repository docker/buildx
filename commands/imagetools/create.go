package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/docker/buildx/store"
	"github.com/docker/buildx/store/storeutil"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/cli/cli/command"
	"github.com/docker/distribution/reference"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type createOptions struct {
	builder      string
	files        []string
	tags         []string
	dryrun       bool
	actionAppend bool
}

func runCreate(dockerCli command.Cli, in createOptions, args []string) error {
	if len(args) == 0 && len(in.files) == 0 {
		return errors.Errorf("no sources specified")
	}

	if !in.dryrun && len(in.tags) == 0 {
		return errors.Errorf("can't push with no tags specified, please set --tag or --dry-run")
	}

	fileArgs := make([]string, len(in.files))
	for i, f := range in.files {
		dt, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		fileArgs[i] = string(dt)
	}

	args = append(fileArgs, args...)

	tags, err := parseRefs(in.tags)
	if err != nil {
		return err
	}

	if in.actionAppend && len(in.tags) > 0 {
		args = append([]string{in.tags[0]}, args...)
	}

	srcs, err := parseSources(args)
	if err != nil {
		return err
	}

	repos := map[string]struct{}{}

	for _, t := range tags {
		repos[t.Name()] = struct{}{}
	}

	sourceRefs := false
	for _, s := range srcs {
		if s.Ref != nil {
			repos[s.Ref.Name()] = struct{}{}
			sourceRefs = true
		}
	}

	if len(repos) == 0 {
		return errors.Errorf("no repositories specified, please set a reference in tag or source")
	}

	var defaultRepo *string
	if len(repos) == 1 {
		for repo := range repos {
			defaultRepo = &repo
		}
	}

	for i, s := range srcs {
		if s.Ref == nil && s.Desc.MediaType == "" && s.Desc.Digest != "" {
			if defaultRepo == nil {
				return errors.Errorf("multiple repositories specified, cannot infer repository for %q", args[i])
			}

			n, err := reference.ParseNormalizedNamed(*defaultRepo)
			if err != nil {
				return err
			}
			r, err := reference.WithDigest(n, s.Desc.Digest)
			if err != nil {
				return err
			}
			srcs[i].Ref = r
			sourceRefs = true
		}
	}

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

	if sourceRefs {
		eg, ctx2 := errgroup.WithContext(ctx)
		for i, s := range srcs {
			if s.Ref == nil {
				continue
			}
			func(i int) {
				eg.Go(func() error {
					_, desc, err := r.Resolve(ctx2, srcs[i].Ref.String())
					if err != nil {
						return err
					}
					if srcs[i].Desc.Digest == "" {
						srcs[i].Desc = desc
					} else {
						var err error
						srcs[i].Desc, err = mergeDesc(desc, srcs[i].Desc)
						if err != nil {
							return err
						}
					}
					return nil
				})
			}(i)
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	dt, desc, err := r.Combine(ctx, srcs)
	if err != nil {
		return err
	}

	if in.dryrun {
		fmt.Printf("%s\n", dt)
		return nil
	}

	// new resolver cause need new auth
	r = imagetools.New(imageopt)

	for _, t := range tags {
		if err := r.Copy(ctx, srcs, t); err != nil {
			return err
		}

		if err := r.Push(ctx, t, desc, dt); err != nil {
			return err
		}
		fmt.Println(t.String())
	}

	return nil
}

func parseSources(in []string) ([]*imagetools.Source, error) {
	out := make([]*imagetools.Source, len(in))
	for i, in := range in {
		s, err := parseSource(in)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse source %q, valid sources are digests, references and descriptors", in)
		}
		out[i] = s
	}
	return out, nil
}

func parseRefs(in []string) ([]reference.Named, error) {
	refs := make([]reference.Named, len(in))
	for i, in := range in {
		n, err := reference.ParseNormalizedNamed(in)
		if err != nil {
			return nil, err
		}
		refs[i] = n
	}
	return refs, nil
}

func parseSource(in string) (*imagetools.Source, error) {
	// source can be a digest, reference or a descriptor JSON
	dgst, err := digest.Parse(in)
	if err == nil {
		return &imagetools.Source{
			Desc: ocispec.Descriptor{
				Digest: dgst,
			},
		}, nil
	} else if strings.HasPrefix(in, "sha256") {
		return nil, err
	}

	ref, err := reference.ParseNormalizedNamed(in)
	if err == nil {
		return &imagetools.Source{
			Ref: ref,
		}, nil
	} else if !strings.HasPrefix(in, "{") {
		return nil, err
	}

	var s imagetools.Source
	if err := json.Unmarshal([]byte(in), &s.Desc); err != nil {
		return nil, errors.WithStack(err)
	}
	return &s, nil
}

func createCmd(dockerCli command.Cli, opts RootOptions) *cobra.Command {
	var options createOptions

	cmd := &cobra.Command{
		Use:   "create [OPTIONS] [SOURCE] [SOURCE...]",
		Short: "Create a new image based on source images",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = *opts.Builder
			return runCreate(dockerCli, options, args)
		},
	}

	flags := cmd.Flags()
	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Read source descriptor from file")
	flags.StringArrayVarP(&options.tags, "tag", "t", []string{}, "Set reference for new image")
	flags.BoolVar(&options.dryrun, "dry-run", false, "Show final image instead of pushing")
	flags.BoolVar(&options.actionAppend, "append", false, "Append to existing manifest")

	return cmd
}

func mergeDesc(d1, d2 ocispec.Descriptor) (ocispec.Descriptor, error) {
	if d2.Size != 0 && d1.Size != d2.Size {
		return ocispec.Descriptor{}, errors.Errorf("invalid size mismatch for %s, %d != %d", d1.Digest, d2.Size, d1.Size)
	}
	if d2.MediaType != "" {
		d1.MediaType = d2.MediaType
	}
	if len(d2.Annotations) != 0 {
		d1.Annotations = d2.Annotations // no merge so support removes
	}
	if d2.Platform != nil {
		d1.Platform = d2.Platform // missing items filled in later from image config
	}
	return d1, nil
}
