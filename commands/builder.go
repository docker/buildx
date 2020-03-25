package commands

import "github.com/spf13/pflag"

type builderOptions struct {
	builder string
}

func builderFlags(options *builderOptions, flags *pflag.FlagSet) {
	flags.StringVar(&options.builder, "builder", "", "Override the configured builder instance")
}
