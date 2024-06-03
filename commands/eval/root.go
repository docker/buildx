package eval

import (
	"fmt"
	"strings"

	"github.com/docker/buildx/util/cobrautil"
	"github.com/spf13/cobra"
)

type Config struct {
	Request string
}

type Cmd interface {
	NewEval(config *Config) *cobra.Command
}

func RootCmd(children ...Cmd) *cobra.Command {
	var options Config

	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Evaluate frontend request",
		Args:  cobra.NoArgs,
	}

	flags := cmd.Flags()
	flags.StringVar(&options.Request, "request", "", fmt.Sprintf("Request to evaluate (%s)", strings.Join(frontendRequests(), ", ")))
	cmd.MarkFlagRequired("request") // TODO: validation does not seem to work: https://github.com/spf13/cobra/issues/921

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	for _, c := range children {
		cmd.AddCommand(c.NewEval(&options))
	}

	return cmd
}

func frontendRequests() []string {
	// TODO: use a more dynamic way to get the list of frontend requests from BuildKit
	return []string{"outline", "targets", "lint"}
}
