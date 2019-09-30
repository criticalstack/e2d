package app

import (
	"github.com/spf13/cobra"
)

var globalOptions struct {
	verbose bool
}

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "e2d",
		Short: "etcd manager",
	}
	cmd.PersistentFlags().BoolVarP(&globalOptions.verbose, "verbose", "v", false, "verbose log output (debug)")

	cmd.AddCommand(
		newCompletionCmd(cmd),
		newRunCmd(),
		newPKICmd(),
		newVersionCmd(),
	)

	return cmd
}
