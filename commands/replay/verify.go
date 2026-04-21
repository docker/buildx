package replay

import (
	"github.com/docker/buildx/replay"
	"github.com/docker/buildx/util/buildflags"
	"github.com/docker/buildx/util/cobrautil/completion"
	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// verifyOptions holds the parsed flags for `replay verify`.
type verifyOptions struct {
	commonOptions
	compare string
	outputs []string
}

func verifyCmd(dockerCli command.Cli, rootOpts RootOptions) *cobra.Command {
	var opts verifyOptions

	cmd := &cobra.Command{
		Use:   "verify [OPTIONS] SUBJECT",
		Short: "Replay a subject and compare the result against the original artifact",
		Args:  cli.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.builder = *rootOpts.Builder
			return runVerify(cmd, dockerCli, &opts, args[0])
		},
		ValidArgsFunction:     completion.Disable,
		DisableFlagsInUseLine: true,
	}

	installCommonFlags(cmd, &opts.commonOptions)

	flags := cmd.Flags()
	flags.StringVar(&opts.compare, "compare", "digest", `Comparison mode ("digest" | "artifact" | "semantic")`)
	flags.StringArrayVarP(&opts.outputs, "output", "o", nil, `Output destination for the verification result (VSA) (format: "type=local,dest=path" | "type=oci,dest=file" | "type=attest")`)

	return cmd
}

// runVerify wires the CLI flags to replay.Verify. Per-subject verification
// runs on a single platform at a time; when an input is multi-platform the
// command iterates over the loaded subjects and returns the first non-nil
// error so the caller's exit code is well-defined.
func runVerify(cmd *cobra.Command, dockerCli command.Cli, opts *verifyOptions, input string) error {
	ctx := cmd.Context()

	mode := opts.compare
	switch mode {
	case "", replay.CompareModeDigest, replay.CompareModeArtifact:
		// ok
	case replay.CompareModeSemantic:
		// Short-circuit the predicate load: semantic comparison is not
		// yet implemented, so the user should see the typed
		// ErrNotImplemented regardless of the subject's shape.
		return replay.ErrNotImplemented("--compare=semantic")
	default:
		return errors.Errorf("unknown --compare %q", opts.compare)
	}

	// Parse --output (optional).
	var exportSpec *buildflags.ExportEntry
	if len(opts.outputs) > 0 {
		specs, err := buildflags.ParseExports(opts.outputs)
		if err != nil {
			return errors.Wrap(err, "parse --output")
		}
		if len(specs) != 1 {
			return errors.Errorf("verify: exactly one --output is required (got %d)", len(specs))
		}
		exportSpec = specs[0]
	}

	resolver, err := replay.NewMaterialsResolver(opts.materials)
	if err != nil {
		return err
	}
	secretSpecs, err := buildflags.ParseSecretSpecs(opts.secrets)
	if err != nil {
		return errors.Wrap(err, "parse --secret")
	}
	sshSpecs, err := buildflags.ParseSSHSpecs(opts.ssh)
	if err != nil {
		return errors.Wrap(err, "parse --ssh")
	}

	subjects, err := replay.LoadSubjects(ctx, dockerCli, opts.builder, input)
	if err != nil {
		return err
	}
	subjects, err = filterSubjectsByPlatform(subjects, opts.platforms)
	if err != nil {
		return err
	}
	if len(subjects) == 0 {
		return errors.New("no subjects matched the --platform filter")
	}

	for _, s := range subjects {
		pred, err := s.Predicate(ctx)
		if err != nil {
			return err
		}
		req := &replay.VerifyRequest{
			Subject:   s,
			Predicate: pred,
			Mode:      mode,
			Materials: resolver,
			Network:   opts.network,
			Secrets:   secretSpecs,
			SSH:       sshSpecs,
			Output:    exportSpec,
		}
		if _, err := replay.Verify(ctx, dockerCli, opts.builder, req); err != nil {
			return err
		}
	}
	return nil
}
