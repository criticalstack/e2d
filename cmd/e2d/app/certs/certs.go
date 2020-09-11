package certs

import (
	"github.com/spf13/cobra"

	certsgenerate "github.com/criticalstack/e2d/cmd/e2d/app/certs/generate"
	certsinit "github.com/criticalstack/e2d/cmd/e2d/app/certs/init"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "certs",
		Short: "manage e2d certs",
	}
	cmd.AddCommand(
		certsinit.NewCommand(),
		certsgenerate.NewCommand(),
	)
	return cmd
}
