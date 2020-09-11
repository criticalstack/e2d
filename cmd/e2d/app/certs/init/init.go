package init

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/criticalstack/e2d/pkg/manager"
)

var opts struct {
	CertDir string
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "init",
		Short:         "initialize a new CA",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.CertDir != "" {
				if err := os.MkdirAll(opts.CertDir, 0755); err != nil && !os.IsExist(err) {
					return err
				}
			}
			return manager.WriteNewCA(opts.CertDir)
		},
	}

	cmd.Flags().StringVar(&opts.CertDir, "cert-dir", "", "")
	return cmd
}
