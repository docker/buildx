package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/docker/buildx/builder"
	"github.com/docker/buildx/policy"
	"github.com/docker/buildx/util/cobrautil"
	"github.com/docker/buildx/util/confutil"
	"github.com/docker/buildx/util/sourcemeta"
	"github.com/docker/cli/cli/command"
	gwpb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func testCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts policy.TestOptions
	cmd := &cobra.Command{
		Use:                   "test <path>",
		Short:                 "Run policy tests",
		Args:                  cobra.ExactArgs(1),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			optionsProvider := newPolicyTestOptionsProvider(dockerCli, rootOpts.Builder)
			opts.Provider = optionsProvider.TestOptionsProvider()
			defer optionsProvider.Close()
			return runTest(cmd.Context(), cmd.OutOrStdout(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Run, "run", "", "Run only tests with name containing this substring")
	cmd.Flags().StringVar(&opts.Filename, "filename", "Dockerfile", "Name of the Dockerfile to validate")
	return cmd
}

func runTest(ctx context.Context, out io.Writer, path string, opts policy.TestOptions) error {
	root := os.DirFS(".")
	statFS, ok := root.(fs.StatFS)
	if !ok {
		return errors.New("policy test root does not support stat")
	}
	opts.Root = statFS

	summary, err := policy.RunPolicyTests(ctx, path, opts)
	if err != nil {
		return err
	}

	for _, result := range summary.Results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		allowStr := "n/a"
		if result.Allow != nil {
			allowStr = fmt.Sprintf("%v", *result.Allow)
		}
		if len(result.DenyMessages) > 0 {
			_, _ = fmt.Fprintf(out, "%s: %s (allow=%s, deny_msg=%s)\n", result.Name, status, allowStr, strings.Join(result.DenyMessages, "; "))
		} else {
			_, _ = fmt.Fprintf(out, "%s: %s (allow=%s)\n", result.Name, status, allowStr)
		}

		if result.Passed {
			continue
		}

		if result.Input != nil {
			writeJSON(out, "input", result.Input)
		} else {
			_, _ = fmt.Fprintln(out, "input: <nil>")
		}
		if result.Decision != nil {
			writeJSON(out, "decision", result.Decision)
		} else {
			_, _ = fmt.Fprintln(out, "decision: <nil>")
		}
		if len(result.MissingInput) > 0 {
			_, _ = fmt.Fprintf(out, "missing_input: %s\n", strings.Join(withInputPrefix(result.MissingInput), ", "))
		}
		if len(result.MetadataNeeded) > 0 {
			_, _ = fmt.Fprintf(out, "metadata_resolve: %s\n", strings.Join(result.MetadataNeeded, ", "))
		}
	}

	if summary.Failed > 0 {
		return cobrautil.ExitCodeError(1)
	}
	return nil
}

func writeJSON(out io.Writer, label string, v any) {
	dt, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		_, _ = fmt.Fprintf(out, "%s: <error encoding>\n", label)
		return
	}
	_, _ = fmt.Fprintf(out, "%s:\n%s\n", label, string(dt))
}

func withInputPrefix(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = "input." + k
	}
	return out
}

type policyTestOptionsProvider struct {
	dockerCli   command.Cli
	builderName *string

	once         sync.Once
	platform     *ocispecs.Platform
	metaResolver *sourcemeta.Resolver
	err          error
}

func newPolicyTestOptionsProvider(dockerCli command.Cli, builderName *string) *policyTestOptionsProvider {
	return &policyTestOptionsProvider{
		dockerCli:   dockerCli,
		builderName: builderName,
	}
}

func (r *policyTestOptionsProvider) TestOptionsProvider() *policy.TestOptionsProvider {
	return &policy.TestOptionsProvider{
		Resolve:          r.Resolve,
		Platform:         r.Platform,
		VerifierProvider: policy.SignatureVerifier(confutil.NewConfig(r.dockerCli)),
	}
}

func (r *policyTestOptionsProvider) Close() error {
	if r.metaResolver == nil {
		return nil
	}
	return r.metaResolver.Close()
}

func (r *policyTestOptionsProvider) Platform(ctx context.Context) (*ocispecs.Platform, error) {
	if err := r.init(ctx); err != nil {
		return nil, err
	}
	return r.platform, nil
}

func (r *policyTestOptionsProvider) Resolve(ctx context.Context, source *pb.SourceOp, req *gwpb.ResolveSourceMetaRequest) (*gwpb.ResolveSourceMetaResponse, error) {
	if err := r.init(ctx); err != nil {
		return nil, err
	}
	opt := sourceResolverOpt(req, r.platform)
	resp, err := r.metaResolver.ResolveSourceMetadata(ctx, source, opt)
	if err != nil {
		return nil, err
	}
	return buildSourceMetaResponse(resp), nil
}

func (r *policyTestOptionsProvider) init(ctx context.Context) error {
	r.once.Do(func() {
		bopts := []builder.Option{}
		if r.builderName != nil {
			bopts = append(bopts, builder.WithName(*r.builderName))
		}
		b, err := builder.New(r.dockerCli, bopts...)
		if err != nil {
			r.err = err
			return
		}

		nodes, err := b.LoadNodes(ctx)
		if err != nil {
			r.err = err
			return
		}
		c, err := nodes[0].Driver.Client(ctx)
		if err != nil {
			r.err = err
			return
		}
		workers, err := c.ListWorkers(ctx)
		if err != nil {
			r.err = err
			return
		}
		if len(workers) == 0 {
			r.err = errors.New("no workers available in the builder")
			return
		}

		defaultPlatform := workers[0].Platforms[0]
		r.platform = &ocispecs.Platform{
			Architecture: defaultPlatform.Architecture,
			OS:           defaultPlatform.OS,
			Variant:      defaultPlatform.Variant,
		}
		r.metaResolver = sourcemeta.NewResolver(c)
	})
	return r.err
}
