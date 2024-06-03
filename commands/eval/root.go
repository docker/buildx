package eval

import (
	"errors"
	"fmt"
	"strings"

	"github.com/docker/buildx/util/cobrautil"
	"github.com/spf13/cobra"
)

type Config struct {
	Request string

	Lint    bool
	Outline bool
	Targets bool
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
	flags.BoolVar(&options.Lint, "lint", false, `Shorthand for "--request=lint"`)
	flags.BoolVar(&options.Outline, "outline", false, `Shorthand for "--request=outline"`)
	flags.BoolVar(&options.Targets, "targets", false, `Shorthand for "--request=targets"`)

	// hide builder persistent flag for this command
	cobrautil.HideInheritedFlags(cmd, "builder")

	for _, c := range children {
		cmd.AddCommand(c.NewEval(&options))
	}

	return cmd
}

func (c *Config) ParseRequest() (string, error) {
	var request string
	count := 0
	if c.Request != "" {
		request = c.Request
		count++
	}
	if c.Lint {
		request = "lint"
		count++
	}
	if c.Outline {
		request = "outline"
		count++
	}
	if c.Targets {
		request = "targets"
		count++
	}
	if count != 1 {
		return "", errors.New("exactly one of --request, --lint, --outline, or --targets must be set")
	}
	return request, nil
}

func frontendRequests() []string {
	// TODO: use a more dynamic way to get the list of frontend requests from BuildKit
	return []string{"outline", "targets", "lint"}
}
