package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.etcd.io/etcd/version"
)

func newVersionCmd() *cobra.Command {
	// TODO(chris): expose e2d version alongside of etcd version
	cmd := &cobra.Command{
		Use:   "version",
		Short: "etcd version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(version.APIVersion)
		},
	}
	return cmd
}
