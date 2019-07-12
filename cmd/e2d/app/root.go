package app

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var RootCmd = &cobra.Command{
	Use:   "e2d",
	Short: "etcd manager",
}

func init() {
	RootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose log output (debug)")
	viper.BindPFlags(RootCmd.PersistentFlags())
	RootCmd.AddCommand(
		completionCmd,
		runCmd,
		pkiCmd,
		versionCmd,
	)
}

func Execute() error {
	return RootCmd.Execute()
}
