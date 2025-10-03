package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/platforms"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/buildx/util/imagetools"
	"github.com/docker/buildx/util/progress"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/util/attestation"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/opencontainers/go-digest"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type createOptions struct {
	builder      string
	files        []string
	tags         []string
	annotations  []string
	dryrun       bool
	actionAppend bool
	progress     string
	preferIndex  bool
	platforms    []string
}

func runCreate(ctx context.Context, dockerCli command.Cli, in createOptions, args []string) error {
	if len(args) == 0 && len(in.files) == 0 {
		return errors.Errorf("no sources specified")
	}

	if !in.dryrun && len(in.tags) == 0 {
		return errors.Errorf("can't push with no tags specified, please set --tag or --dry-run")
	}

	fileArgs := make([]string, len(in.files), len(in.files)+len(args))
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

	platforms, err := parsePlatforms(in.platforms)
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
		if s.Ref == nil {
			if defaultRepo == nil {
				return errors.Errorf("multiple repositories specified, cannot infer repository for %q", args[i])
			}
			n, err := reference.ParseNormalizedNamed(*defaultRepo)
			if err != nil {
				return err
			}
			if s.Desc.MediaType == "" && s.Desc.Digest != "" {
				r, err := reference.WithDigest(n, s.Desc.Digest)
				if err != nil {
					return err
				}
				srcs[i].Ref = r
				sourceRefs = true
			} else {
				srcs[i].Ref = reference.TagNameOnly(n)
			}
		}
	}

	b, err := builder.New(dockerCli, builder.WithName(in.builder))
	if err != nil {
		return err
	}
	imageopt, err := b.ImageOpt()
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

	annotations, err := buildflags.ParseAnnotations(in.annotations)
	if err != nil {
		return errors.Wrapf(err, "failed to parse annotations")
	}

	dt, desc, srcMap, err := r.Combine(ctx, srcs, annotations, in.preferIndex)
	if err != nil {
		return err
	}

	dt, desc, manifests, err := filterPlatforms(dt, desc, srcMap, platforms)
	if err != nil {
		return err
	}

	if in.dryrun {
		fmt.Printf("%s\n", dt)
		return nil
	}

	// manifests can be nil only if pushing one single-platform desc directly
	if manifests == nil {
		manifests = []descWithSource{{Descriptor: desc, Source: srcs[0]}}
	}

	// new resolver cause need new auth
	r = imagetools.New(imageopt)

	ctx2, cancel := context.WithCancelCause(context.TODO())
	defer func() { cancel(errors.WithStack(context.Canceled)) }()
	progressMode := in.progress
	if progressMode == "none" {
		progressMode = "quiet"
	}
	printer, err := progress.NewPrinter(ctx2, os.Stderr, progressui.DisplayMode(progressMode))
	if err != nil {
		return err
	}

	eg, _ := errgroup.WithContext(ctx)
	pw := progress.WithPrefix(printer, "internal", true)

	for _, t := range tags {
		eg.Go(func() error {
			return progress.Wrap(fmt.Sprintf("pushing %s", t.String()), pw.Write, func(sub progress.SubLogger) error {
				eg2, _ := errgroup.WithContext(ctx)
				for _, desc := range manifests {
					eg2.Go(func() error {
						sub.Log(1, fmt.Appendf(nil, "copying %s from %s to %s\n", desc.Digest.String(), desc.Source.Ref.String(), t.String()))
						return r.Copy(ctx, desc.Source, t)
					})
				}
				if err := eg2.Wait(); err != nil {
					return err
				}
				sub.Log(1, fmt.Appendf(nil, "pushing %s to %s\n", desc.Digest.String(), t.String()))
				return r.Push(ctx, t, desc, dt)
			})
		})
	}

	err = eg.Wait()
	err1 := printer.Wait()
	if err == nil {
		err = err1
	}

	return err
}

type descWithSource struct {
	ocispecs.Descriptor
	Source *imagetools.Source
}

func filterPlatforms(dt []byte, desc ocispecs.Descriptor, srcMap map[digest.Digest]*imagetools.Source, plats []ocispecs.Platform) ([]byte, ocispecs.Descriptor, []descWithSource, error) {
	matcher := platforms.Any(plats...)

	if !images.IsIndexType(desc.MediaType) {
		if len(plats) == 0 {
			return dt, desc, nil, nil
		}
		var mfst ocispecs.Manifest
		if err := json.Unmarshal(dt, &mfst); err != nil {
			return nil, ocispecs.Descriptor{}, nil, errors.Wrapf(err, "failed to parse manifest")
		}
		if desc.Platform == nil {
			return nil, ocispecs.Descriptor{}, nil, errors.Errorf("cannot filter platforms from a manifest without platform information")
		}
		if !matcher.Match(*desc.Platform) {
			return nil, ocispecs.Descriptor{}, nil, errors.Errorf("input platform %s does not match any of the provided platforms", platforms.Format(*desc.Platform))
		}
		return dt, desc, nil, nil
	}

	var idx ocispecs.Index
	if err := json.Unmarshal(dt, &idx); err != nil {
		return nil, ocispecs.Descriptor{}, nil, errors.Wrapf(err, "failed to parse index")
	}
	if len(plats) == 0 {
		mfsts := make([]descWithSource, len(idx.Manifests))
		for i, m := range idx.Manifests {
			src, ok := srcMap[m.Digest]
			if !ok {
				defaultSource, ok := srcMap[desc.Digest]
				if !ok {
					return nil, ocispecs.Descriptor{}, nil, errors.Errorf("internal error: no source found for %s", m.Digest)
				}
				src = defaultSource
			}
			mfsts[i] = descWithSource{
				Descriptor: m,
				Source:     src,
			}
		}
		return dt, desc, mfsts, nil
	}

	manifestMap := map[digest.Digest]ocispecs.Descriptor{}
	for _, m := range idx.Manifests {
		manifestMap[m.Digest] = m
	}
	references := map[digest.Digest]struct{}{}
	for _, m := range idx.Manifests {
		if refType, ok := m.Annotations[attestation.DockerAnnotationReferenceType]; ok && refType == attestation.DockerAnnotationReferenceTypeDefault {
			dgstStr, ok := m.Annotations[attestation.DockerAnnotationReferenceDigest]
			if !ok {
				continue
			}
			dgst, err := digest.Parse(dgstStr)
			if err != nil {
				continue
			}
			subject, ok := manifestMap[dgst]
			if !ok {
				continue
			}
			if subject.Platform == nil || matcher.Match(*subject.Platform) {
				references[m.Digest] = struct{}{}
			}
		}
	}

	var mfsts []ocispecs.Descriptor
	var mfstsWithSource []descWithSource

	for _, m := range idx.Manifests {
		if _, isRef := references[m.Digest]; isRef || m.Platform == nil || matcher.Match(*m.Platform) {
			src, ok := srcMap[m.Digest]
			if !ok {
				defaultSource, ok := srcMap[desc.Digest]
				if !ok {
					return nil, ocispecs.Descriptor{}, nil, errors.Errorf("internal error: no source found for %s", m.Digest)
				}
				src = defaultSource
			}
			mfsts = append(mfsts, m)
			mfstsWithSource = append(mfstsWithSource, descWithSource{
				Descriptor: m,
				Source:     src,
			})
		}
	}
	if len(mfsts) == len(idx.Manifests) {
		// all platforms matched, no need to rewrite index
		return dt, desc, mfstsWithSource, nil
	}

	if len(mfsts) == 0 {
		return nil, ocispecs.Descriptor{}, nil, errors.Errorf("none of the manifests match the provided platforms")
	}

	idx.Manifests = mfsts
	idxBytes, err := json.MarshalIndent(&idx, "", "  ")
	if err != nil {
		return nil, ocispecs.Descriptor{}, nil, errors.Wrap(err, "failed to marshal index")
	}

	desc = ocispecs.Descriptor{
		MediaType:   desc.MediaType,
		Size:        int64(len(idxBytes)),
		Digest:      digest.FromBytes(idxBytes),
		Annotations: desc.Annotations,
	}

	return idxBytes, desc, mfstsWithSource, nil
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

func parsePlatforms(in []string) ([]ocispecs.Platform, error) {
	out := make([]ocispecs.Platform, 0, len(in))
	for _, p := range in {
		if arr := strings.Split(p, ","); len(arr) > 1 {
			v, err := parsePlatforms(arr)
			if err != nil {
				return nil, err
			}
			out = append(out, v...)
			continue
		}
		plat, err := platforms.Parse(p)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid platform %q", p)
		}
		out = append(out, plat)
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
			Desc: ocispecs.Descriptor{
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
		Use:   "create [OPTIONS] [SOURCE...]",
		Short: "Create a new image based on source images",
		RunE: func(cmd *cobra.Command, args []string) error {
			options.builder = *opts.Builder
			return runCreate(cmd.Context(), dockerCli, options, args)
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	flags := cmd.Flags()
	flags.StringArrayVarP(&options.files, "file", "f", []string{}, "Read source descriptor from file")
	flags.StringArrayVarP(&options.tags, "tag", "t", []string{}, "Set reference for new image")
	flags.BoolVar(&options.dryrun, "dry-run", false, "Show final image instead of pushing")
	flags.BoolVar(&options.actionAppend, "append", false, "Append to existing manifest")
	flags.StringVar(&options.progress, "progress", "auto", `Set type of progress output ("auto", "none", "plain", "rawjson", "tty"). Use plain to show container output`)
	flags.StringArrayVarP(&options.annotations, "annotation", "", []string{}, "Add annotation to the image")
	flags.BoolVar(&options.preferIndex, "prefer-index", true, "When only a single source is specified, prefer outputting an image index or manifest list instead of performing a carbon copy")
	flags.StringArrayVarP(&options.platforms, "platform", "p", []string{}, "Filter specified platforms of target image")

	return cmd
}

func mergeDesc(d1, d2 ocispecs.Descriptor) (ocispecs.Descriptor, error) {
	if d2.Size != 0 && d1.Size != d2.Size {
		return ocispecs.Descriptor{}, errors.Errorf("invalid size mismatch for %s, %d != %d", d1.Digest, d2.Size, d1.Size)
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
