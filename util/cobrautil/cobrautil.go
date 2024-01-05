package cobrautil

import (
	"context"
	"os"
	"os/signal"

	"github.com/moby/buildkit/util/bklog"
	detect "github.com/moby/buildkit/util/tracing/env"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// HideInheritedFlags hides inherited flags
func HideInheritedFlags(cmd *cobra.Command, hidden ...string) {
	for _, h := range hidden {
		// we could use cmd.SetHelpFunc to override the helper
		// but, it's not enough because we also want the generated
		// docs to be updated, so we override the flag instead
		cmd.Flags().String(h, "", "")
		_ = cmd.Flags().MarkHidden(h)
	}
}

const annotationExperimentalCLI = "experimentalCLI"

func MarkFlagExperimental(f *pflag.Flag) {
	if _, ok := f.Annotations[annotationExperimentalCLI]; ok {
		return
	}
	if f.Annotations == nil {
		f.Annotations = make(map[string][]string)
	}
	f.Annotations[annotationExperimentalCLI] = nil
	f.Usage += " (EXPERIMENTAL)"
}

func MarkFlagsExperimental(fs *pflag.FlagSet, names ...string) {
	for _, name := range names {
		f := fs.Lookup(name)
		if f == nil {
			logrus.Warningf("Unknown flag name %q", name)
			continue
		}
		MarkFlagExperimental(f)
	}
}

func MarkCommandExperimental(c *cobra.Command) {
	if _, ok := c.Annotations[annotationExperimentalCLI]; ok {
		return
	}
	if c.Annotations == nil {
		c.Annotations = make(map[string]string)
	}
	c.Annotations[annotationExperimentalCLI] = ""
	c.Short += " (EXPERIMENTAL)"
}

// ConfigureContext sets up signal handling and hooks into the command's
// context so that it will be cancelled when signalled, as well as implementing
// the "hard exit after 3 signals" logic. It also configures OTEL tracing
// for the relevant context.
func ConfigureContext(fn func(*cobra.Command, []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := detect.InitContext(cmd.Context())
		cancellableCtx, cancel := context.WithCancelCause(ctx)
		ctx = cancellableCtx

		signalLimit := 3
		s := make(chan os.Signal, signalLimit)
		signal.Notify(s, interruptSignals...)
		go func() {
			retries := 0
			for {
				<-s
				retries++
				err := errors.Errorf("got %d SIGTERM/SIGINTs, forcing shutdown", retries)
				cancel(err)
				if retries >= signalLimit {
					bklog.G(ctx).Errorf(err.Error())
					os.Exit(1)
				}
			}
		}()

		cmd.SetContext(ctx)
		return fn(cmd, args)
	}
}
