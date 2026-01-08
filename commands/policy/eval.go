package policy

import (
	"os"
	"strings"

	"github.com/moby/buildkit/frontend/dockerui"
	"github.com/moby/buildkit/solver/pb"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func evalCmd() *cobra.Command {
	var filename string
	var printOutput bool

	cmd := &cobra.Command{
		Use:   "eval [source]",
		Short: "Evaluate policy for a source",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEval(args, filename, printOutput)
		},
	}
	cmd.Flags().StringVar(&filename, "filename", "", "Policy filename to evaluate")
	cmd.Flags().BoolVar(&printOutput, "print", false, "Print policy output")

	return cmd
}

func runEval(args []string, filename string, printOutput bool) error {
	if len(args) > 0 {
		if _, err := parseSource(args[0]); err != nil {
			return err
		}
	}
	_ = filename
	_ = printOutput
	return errors.New("not implemented")
}

func parseSource(input string) (*pb.SourceOp, error) {
	if strings.HasPrefix(input, "docker-image://") {
		return &pb.SourceOp{Identifier: input}, nil
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
		if err != nil {
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
