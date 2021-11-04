package cobrautil

import "github.com/spf13/cobra"

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
