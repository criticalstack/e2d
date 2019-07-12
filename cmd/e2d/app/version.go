package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.etcd.io/etcd/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "etcd version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(version.APIVersion)
	},
}
