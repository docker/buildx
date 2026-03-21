package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/sourcemeta"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/frontend/dockerui"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type evalOpts struct {
	filename    string
	printOutput bool
	fields      []string
	platform    string
	builder     *string
}

func evalCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts evalOpts

	cmd := &cobra.Command{
		Use:                   "eval [OPTIONS] source",
		Short:                 "Evaluate policy for a source",
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builder = rootOpts.Builder
			return runEval(cmd.Context(), dockerCli, args[0], opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVarP(&opts.filename, "file", "f", "Dockerfile", "Policy filename to evaluate")
	flags.BoolVar(&opts.printOutput, "print", false, "Print policy output")
	flags.StringSliceVar(&opts.fields, "fields", nil, "Fields to evaluate")
	flags.StringVar(&opts.platform, "platform", "", "Target platform for policy evaluation")
	// Deprecated: use --file instead
	flags.StringVar(&opts.filename, "filename", "Dockerfile", "Policy filename to evaluate")
	flags.MarkHidden("filename")
	return cmd
}

func runEval(ctx context.Context, dockerCli command.Cli, source string, opts evalOpts) error {
	src, err := parseSource(source)
	if err != nil {
		return err
	}

	bopts := []builder.Option{}
	if opts.builder != nil {
		bopts = append(bopts, builder.WithName(*opts.builder))
	}

	b, err := builder.New(dockerCli, bopts...)
	if err != nil {
		return err
	}

	nodes, err := b.LoadNodes(ctx)
	if err != nil {
		return err
	}

	c, err := nodes[0].Driver.Client(ctx)
	if err != nil {
		return err
	}

	var p ocispecs.Platform
	if opts.platform != "" {
		parsedPlatform, err := parsePlatform(opts.platform)
		if err != nil {
			return err
		}
		p = *parsedPlatform
	} else {
		workers, err := c.ListWorkers(ctx)
		if err != nil {
			return err
		}

		if len(workers) == 0 {
			return errors.New("no workers available in the builder")
		}

		p = workers[0].Platforms[0]
	}
	metaResolver := sourcemeta.NewResolver(c)
	defer metaResolver.Close()

	platform := toPBPlatform(p)
	verifier := policy.SignatureVerifier(confutil.NewConfig(dockerCli))

	if opts.printOutput {
		srcReq := &gwpb.ResolveSourceMetaResponse{
			Source: src,
		}
		input, err := policy.SourceToInput(ctx, verifier, srcReq, &p, nil)
		if err != nil {
			return err
		}
		maxAttempts := 5
		var lastUnknowns []string
		var trimmedUnknowns []string
		var invalidFields []string
		reloadedFields := map[string]struct{}{}
		for {
			maxAttempts--
			if maxAttempts <= 0 {
				return errors.New("maximum attempts reached for resolving source metadata")
			}
			unknowns := input.Unknowns()
			trimmedUnknowns = make([]string, 0, len(unknowns))
			for _, u := range unknowns {
				trimmedUnknowns = append(trimmedUnknowns, strings.TrimPrefix(u, "input."))
			}
			if lastUnknowns != nil && slices.Equal(trimmedUnknowns, lastUnknowns) {
				break
			}
			lastUnknowns = slices.Clone(trimmedUnknowns)
			toReload, invalid := selectReloadFields(opts.fields, trimmedUnknowns)
			for _, f := range toReload {
				reloadedFields[f] = struct{}{}
			}
			invalidFields = invalid
			if len(toReload) > 0 {
				retry, next, err := policy.ResolveInputUnknowns(ctx, &input, srcReq.Source, toReload, platform, &p, metaResolver, verifier, nil)
				if err != nil {
					return err
				}
				if next != nil {
					resp, err := metaResolver.ResolveSourceMetadata(ctx, next.Source, sourcemeta.ToResolverOpt(next, &p))
					if err != nil {
						return err
					}
					srcReq = sourcemeta.ToGatewayMetaResponse(resp)
					input, err = policy.SourceToInput(ctx, verifier, srcReq, &p, nil)
					if err != nil {
						return err
					}
					continue
				}
				if retry {
					continue
				}
			}
			break
		}
		invalidFields = filterInvalidFields(invalidFields, reloadedFields)

		if len(invalidFields) > 0 {
			logrus.Warnf("invalid fields: %v", strings.Join(invalidFields, ", "))
		}
		reportedUnknowns := summarizeEvalUnknowns(trimmedUnknowns, opts.fields)
		if len(reportedUnknowns) > 0 {
			logrus.Infof("unresolved fields: %v", strings.Join(reportedUnknowns, ", "))
		}

		printInput := input
		sanitizePrintInput(&printInput)

		dt, err := json.MarshalIndent(printInput, "", "  ")
		if err != nil {
			return errors.Wrap(err, "failed to marshal policy input")
		}
		_, _ = fmt.Fprintln(os.Stdout, string(dt))
		return nil
	}

	if opts.filename == "" {
		return errors.New("filename is required")
	}
	policyName, policyFile := policyFileNames(opts.filename)
	policyData, err := readPolicyData(policyFile, os.Stdin)
	if err != nil {
		return errors.Wrapf(err, "failed to read policy file %s", policyFile)
	}
	fsProvider := func() (fs.StatFS, func() error, error) {
		root, err := os.OpenRoot(".")
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to open root for policy file %s", policyFile)
		}
		baseFS := root.FS()
		statFS, ok := baseFS.(fs.StatFS)
		if !ok {
			_ = root.Close()
			return nil, nil, errors.Errorf("invalid root FS type %T", baseFS)
		}
		return statFS, root.Close, nil
	}

	env := policy.Env{
		Filename: filepath.Base(policyName),
	}
	policyLog := func(_ logrus.Level, msg string) {
		logrus.Debug(msg)
	}

	policyEval := policy.NewPolicy(policy.Opt{
		Files: []policy.File{
			{
				Filename: filepath.Base(policyFile),
				Data:     policyData,
			},
		},
		Env:              env,
		Log:              policyLog,
		FS:               fsProvider,
		VerifierProvider: verifier,
		DefaultPlatform:  &p,
		SourceResolver:   metaResolver,
	})

	srcReq := &gwpb.ResolveSourceMetaResponse{
		Source: src,
	}
	maxAttempts := 5
	for {
		maxAttempts--
		if maxAttempts <= 0 {
			return errors.New("maximum attempts reached for resolving policy metadata")
		}

		decision, next, err := policyEval.CheckPolicy(ctx, &policysession.CheckPolicyRequest{
			Platform: platform,
			Source:   srcReq,
		})
		if err != nil {
			return err
		}
		if next == nil {
			return evalDecisionError(decision)
		}

		opt := sourcemeta.ToResolverOpt(next, &p)
		target := src
		if next.Source != nil {
			target = next.Source
		}
		resp, err := metaResolver.ResolveSourceMetadata(ctx, target, opt)
		if err != nil {
			return err
		}
		srcReq = sourcemeta.ToGatewayMetaResponse(resp)
	}
}

func policyFileNames(filename string) (string, string) {
	if filename == "-" {
		return "stdin", filename
	}
	return filename, filename + ".rego"
}

func readPolicyData(filename string, stdin io.Reader) ([]byte, error) {
	if filename == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(filename)
}

func selectReloadFields(fields []string, unknowns []string) ([]string, []string) {
	if len(fields) == 0 {
		return nil, nil
	}
	reload := map[string]struct{}{}
	var invalid []string
	for _, field := range fields {
		if prereq, ok := materialFieldPrerequisites(field); ok {
			added := false
			for _, p := range prereq {
				if slices.Contains(unknowns, p) {
					reload[p] = struct{}{}
					added = true
				}
			}
			if slices.Contains(unknowns, field) {
				reload[field] = struct{}{}
				added = true
			} else if ancestor := findUnknownAncestor(field, unknowns); ancestor != "" {
				reload[ancestor] = struct{}{}
				added = true
			}
			if !added {
				invalid = append(invalid, field)
			}
			continue
		}
		if slices.Contains(unknowns, field) {
			reload[field] = struct{}{}
			continue
		}
		invalid = append(invalid, field)
	}
	return slices.Collect(maps.Keys(reload)), invalid
}

func filterInvalidFields(invalid []string, reloadedFields map[string]struct{}) []string {
	if len(invalid) == 0 {
		return nil
	}
	out := make([]string, 0, len(invalid))
	for _, field := range invalid {
		if _, ok := reloadedFields[field]; ok {
			continue
		}
		out = append(out, field)
	}
	return out
}

func findUnknownAncestor(field string, unknowns []string) string {
	var best string
	for _, unknown := range unknowns {
		if field == unknown {
			return unknown
		}
		if strings.HasPrefix(field, unknown+".") {
			if len(unknown) > len(best) {
				best = unknown
			}
			continue
		}
		if strings.HasPrefix(field, unknown+"[") {
			if len(unknown) > len(best) {
				best = unknown
			}
		}
	}
	return best
}

func materialFieldPrerequisites(field string) ([]string, bool) {
	const seg = ".image.provenance.materials["
	if !strings.HasPrefix(field, seg[1:]) {
		return nil, false
	}
	provenancePath := strings.TrimSuffix(seg, ".materials[")
	out := map[string]struct{}{strings.TrimPrefix(provenancePath, "."): {}}
	collectMaterialPrerequisites(field, seg, provenancePath, 0, out)
	keys := slices.Collect(maps.Keys(out))
	slices.Sort(keys)
	return keys, true
}

func collectMaterialPrerequisites(field, seg, provenancePath string, start int, out map[string]struct{}) {
	i := strings.Index(field[start:], seg)
	if i < 0 {
		return
	}
	i += start
	out[field[:i]+provenancePath] = struct{}{}
	collectMaterialPrerequisites(field, seg, provenancePath, i+len(seg), out)
}

func summarizeEvalUnknowns(unknowns, requested []string) []string {
	if len(unknowns) == 0 {
		return nil
	}
	if len(requested) > 0 {
		out := map[string]struct{}{}
		for _, field := range requested {
			if slices.Contains(unknowns, field) {
				out[field] = struct{}{}
				continue
			}
			if ancestor := findUnknownAncestor(field, unknowns); ancestor != "" {
				out[ancestor] = struct{}{}
			}
		}
		keys := slices.Collect(maps.Keys(out))
		slices.Sort(keys)
		return keys
	}

	out := map[string]struct{}{}
	for _, u := range unknowns {
		out[summarizeUnknownField(u)] = struct{}{}
	}
	keys := slices.Collect(maps.Keys(out))
	slices.Sort(keys)
	return keys
}

func summarizeUnknownField(field string) string {
	if base, _, ok := strings.Cut(field, ".materials["); ok {
		return base + ".materials"
	}
	if strings.HasPrefix(field, "materials[") {
		return "materials"
	}
	parts := strings.Split(field, ".")
	if len(parts) > 1 {
		return strings.Join(parts[:2], ".")
	}
	return field
}

func sanitizePrintInput(inp *policy.Input) {
	if inp == nil {
		return
	}
	inp.Env.Depth = 0
	if inp.Image == nil || inp.Image.Provenance == nil || len(inp.Image.Provenance.Materials) == 0 {
		return
	}
	for i := range inp.Image.Provenance.Materials {
		sanitizePrintInput(&inp.Image.Provenance.Materials[i])
	}
}

func evalDecisionError(decision *policysession.DecisionResponse) error {
	if decision == nil {
		return errors.New("policy returned no decision")
	}
	switch decision.Action {
	case spb.PolicyAction_ALLOW, spb.PolicyAction_CONVERT:
		return nil
	case spb.PolicyAction_DENY:
		if len(decision.DenyMessages) == 0 {
			return errors.New("policy denied")
		}
		msgs := make([]string, 0, len(decision.DenyMessages))
		for _, msg := range decision.DenyMessages {
			if msg != nil && msg.Message != "" {
				msgs = append(msgs, msg.Message)
			}
		}
		if len(msgs) == 0 {
			return errors.New("policy denied")
		}
		return errors.Errorf("policy denied: %s", strings.Join(msgs, "; "))
	default:
		return errors.Errorf("unknown policy action %s", decision.Action)
	}
}

func parseSource(input string) (*pb.SourceOp, error) {
	if refstr, ok := strings.CutPrefix(input, "docker-image://"); ok {
		ref, err := reference.ParseNormalizedNamed(refstr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse image source reference")
		}
		ref = reference.TagNameOnly(ref)
		return &pb.SourceOp{Identifier: "docker-image://" + ref.String()}, nil
	}
	if strings.HasPrefix(input, "git://") {
		_, ok, err := dockerui.DetectGitContext(input, nil)
		if !ok {
			return nil, errors.Errorf("invalid git context %s", input)
		}
		if err != nil {
			return nil, err
		}
		return &pb.SourceOp{Identifier: input}, nil
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		_, ok, err := dockerui.DetectGitContext(input, nil)
		if ok {
			return &pb.SourceOp{
				Identifier: "git://" + input,
				Attrs: map[string]string{
					"git.fullurl": input,
				},
			}, nil
		}
		if err != nil && !errors.Is(err, errdefs.ErrInvalidArgument) {
			return nil, err
		}
		return &pb.SourceOp{Identifier: input}, nil
	}
	// everything else is treated as a local path
	if _, err := os.Stat(input); err != nil {
		return nil, errors.Wrapf(err, "invalid local path %s", input)
	}
	return &pb.SourceOp{Identifier: "local://context"}, nil
}
