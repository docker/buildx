// Command used for prune
package commands

import "fmt"
import "github.com/docker/cli/cli/command"

func pruneCmd(dockerCli command.Cli) *cobra.Command {
	fmt.Println("ASDF - pruneCmd")

	cmd := &cobra.Command{
		Use: "docker buildx prune"
		Short: "Cleans up the build cache"
		Args: cli.NoArgs
		RunE: func(cmd *cobra.Command, args []string) error {
			spaceReclaimed, output, err := runPrune(dockerCli, options)
			if err != nil {
				return err
			}

			if output != "" {
				fmt.Fprintln(dockerCli.Out(), output)
			}
			fmt.Fprintln(dockerCli.Out(), "Total reclaimed space:", units.HumanSize(float64(spaceReclaimed)))
			return nil

		},
		Annotations: map[string]string{"version": "1.00"},
	}

	// ToDo: Uncomment and test this out once we get the base feature working
	//flags := cmd.Flags()
	//flags.BoolVarP(&options.force, "force", "f", false, "Do not prompt for confirmation")
	//flags.Var(&options.filter, "filter", "Provide filter values (e.g. 'until=<timestamp>')")

	const (
		normalWarning   = `WARNING! This will remove all dangling build cache. Are you sure you want to continue?`
		allCacheWarning = `WARNING! This will remove all build cache. Are you sure you want to continue?`
	)

	return cmd

}


func runPrune(dockerCli command.Cli, options pruneOptions) (spaceReclaimed uint64, output string, err error) {
	fmt.Println("ASDF runPrune - Hello world")

	// ToDo: Implement this function

	return 0, "Not implemented", nil
}
