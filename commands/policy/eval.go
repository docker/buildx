package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/cli/cli/command"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/frontend/dockerui"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	spb "github.com/moby/buildkit/sourcepolicy/pb"
	"github.com/moby/buildkit/sourcepolicy/policysession"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type evalOpts struct {
	filename    string
	printOutput bool
	fields      []string
	builder     *string
}

func evalCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts evalOpts

	cmd := &cobra.Command{
		Use:                   "eval source",
		Short:                 "Evaluate policy for a source",
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builder = rootOpts.Builder
			return runEval(cmd.Context(), dockerCli, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.filename, "filename", "Dockerfile", "Policy filename to evaluate")
	cmd.Flags().BoolVar(&opts.printOutput, "print", false, "Print policy output")
	cmd.Flags().StringSliceVar(&opts.fields, "fields", nil, "Fields to evaluate")
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

	workers, err := c.ListWorkers(ctx)
	if err != nil {
		return err
	}

	if len(workers) == 0 {
		return errors.New("no workers available in the builder")
	}

	defaultPlatform := workers[0].Platforms[0]
	p := ocispecs.Platform{
		Architecture: defaultPlatform.Architecture,
		OS:           defaultPlatform.OS,
		Variant:      defaultPlatform.Variant,
	}
	openClient, release, err := gatewayClientFactory(c)
	if err != nil {
		return err
	}
	defer release()

	platform := &pb.Platform{
		Architecture: p.Architecture,
		OS:           p.OS,
		Variant:      p.Variant,
	}
	verifier := policy.SignatureVerifier(confutil.NewConfig(dockerCli))

	if opts.printOutput {
		srcReq := &gwpb.ResolveSourceMetaResponse{
			Source: src,
		}
		maxAttempts := 5
		var unknowns []string
		var lastUnknowns []string
		var trimmedUnknowns []string
		var input policy.Input
		for {
			maxAttempts--
			if maxAttempts <= 0 {
				return errors.New("maximum attempts reached for resolving source metadata")
			}
			input, unknowns, err = policy.SourceToInput(ctx, verifier, srcReq, &p)
			if err != nil {
				return err
			}
			trimmedUnknowns = trimInputPrefixSlice(unknowns)
			if lastUnknowns != nil && slices.Equal(trimmedUnknowns, lastUnknowns) {
				break
			}
			lastUnknowns = slices.Clone(trimmedUnknowns)
			toReload := []string{}
			invalid := []string{}
			for _, f := range opts.fields {
				if slices.Contains(trimmedUnknowns, f) {
					toReload = append(toReload, f)
				} else {
					invalid = append(invalid, f)
				}
			}
			if len(toReload) > 0 {
				req := &gwpb.ResolveSourceMetaRequest{}
				if err := policy.AddUnknowns(req, toReload); err != nil {
					return err
				}
				gwClient, err := openClient(ctx)
				if err != nil {
					return err
				}

				opt := sourceResolverOpt(req, &p)
				resp, err := gwClient.ResolveSourceMetadata(ctx, src, opt)
				if err != nil {
					return err
				}
				srcReq = buildSourceMetaResponse(resp, req)
				continue
			}
			if len(invalid) > 0 {
				logrus.Warnf("invalid fields: %v", strings.Join(invalid, ", "))
			}
			break
		}

		if len(trimmedUnknowns) > 0 {
			logrus.Infof("unresolved fields: %v", strings.Join(trimmedUnknowns, ", "))
		}

		dt, err := json.MarshalIndent(input, "", "  ")
		if err != nil {
			return errors.Wrap(err, "failed to marshal policy input")
		}
		_, _ = fmt.Fprintln(os.Stdout, string(dt))
		return nil
	}

	if opts.filename == "" {
		return errors.New("filename is required")
	}
	policyName := opts.filename
	policyFile := policyName + ".rego"
	policyData, err := os.ReadFile(policyFile)
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

	policyEval := policy.NewPolicy(policy.Opt{
		Files: []policy.File{
			{
				Filename: filepath.Base(policyFile),
				Data:     policyData,
			},
		},
		Env:              env,
		FS:               fsProvider,
		VerifierProvider: verifier,
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

		gwClient, err := openClient(ctx)
		if err != nil {
			return err
		}
		opt := sourceResolverOpt(next, &p)
		resp, err := gwClient.ResolveSourceMetadata(ctx, src, opt)
		if err != nil {
			return err
		}
		srcReq = buildSourceMetaResponse(resp, next)
	}
}

func toGatewayDescriptor(desc ocispecs.Descriptor) *gwpb.Descriptor {
	return &gwpb.Descriptor{
		MediaType:   desc.MediaType,
		Digest:      desc.Digest.String(),
		Size:        desc.Size,
		Annotations: desc.Annotations,
	}
}

func toGatewayAttestationChain(chain *sourceresolver.AttestationChain) *gwpb.AttestationChain {
	if chain == nil {
		return nil
	}
	signatures := make([]string, 0, len(chain.SignatureManifests))
	for _, dgst := range chain.SignatureManifests {
		signatures = append(signatures, dgst.String())
	}
	blobs := make(map[string]*gwpb.Blob, len(chain.Blobs))
	for dgst, blob := range chain.Blobs {
		blobs[dgst.String()] = &gwpb.Blob{
			Descriptor_: toGatewayDescriptor(blob.Descriptor),
			Data:        blob.Data,
		}
	}
	return &gwpb.AttestationChain{
		Root:                chain.Root.String(),
		ImageManifest:       chain.ImageManifest.String(),
		AttestationManifest: chain.AttestationManifest.String(),
		SignatureManifests:  signatures,
		Blobs:               blobs,
	}
}

func sourceResolverOpt(req *gwpb.ResolveSourceMetaRequest, platform *ocispecs.Platform) sourceresolver.Opt {
	opt := sourceresolver.Opt{
		LogName:        req.LogName,
		SourcePolicies: req.SourcePolicies,
	}
	if req.Image != nil {
		opt.ImageOpt = &sourceresolver.ResolveImageOpt{
			NoConfig:         req.Image.NoConfig,
			AttestationChain: req.Image.AttestationChain,
			Platform:         platform,
			ResolveMode:      req.ResolveMode,
		}
	}
	if req.Git != nil {
		opt.GitOpt = &sourceresolver.ResolveGitOpt{
			ReturnObject: req.Git.ReturnObject,
		}
	}
	return opt
}

func buildSourceMetaResponse(resp *sourceresolver.MetaResponse, req *gwpb.ResolveSourceMetaRequest) *gwpb.ResolveSourceMetaResponse {
	out := &gwpb.ResolveSourceMetaResponse{
		Source: resp.Op,
	}
	if resp.Image != nil {
		chain := toGatewayAttestationChain(resp.Image.AttestationChain)
		if chain == nil && req != nil && req.Image != nil && req.Image.AttestationChain {
			chain = &gwpb.AttestationChain{}
		}
		out.Image = &gwpb.ResolveSourceImageResponse{
			Digest:           resp.Image.Digest.String(),
			Config:           resp.Image.Config,
			AttestationChain: chain,
		}
	}
	if resp.Git != nil {
		out.Git = &gwpb.ResolveSourceGitResponse{
			Checksum:       resp.Git.Checksum,
			Ref:            resp.Git.Ref,
			CommitChecksum: resp.Git.CommitChecksum,
			CommitObject:   resp.Git.CommitObject,
			TagObject:      resp.Git.TagObject,
		}
	}
	if resp.HTTP != nil {
		var lastModified *timestamppb.Timestamp
		if resp.HTTP.LastModified != nil {
			lastModified = timestamppb.New(*resp.HTTP.LastModified)
		}
		out.HTTP = &gwpb.ResolveSourceHTTPResponse{
			Checksum:     resp.HTTP.Digest.String(),
			Filename:     resp.HTTP.Filename,
			LastModified: lastModified,
		}
	}
	return out
}

func trimInputPrefixSlice(fields []string) []string {
	if len(fields) == 0 {
		return fields
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, strings.TrimPrefix(field, "input."))
	}
	return out
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
