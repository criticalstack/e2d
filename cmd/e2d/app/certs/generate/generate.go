package generate

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
)

var opts struct {
	CertDir  string
	AltNames string
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate [flags] [arg]\n\nValidArgs:\n  all, server, peer, client",
		Short:   "generate certificates/private keys",
		Aliases: []string{"gen"},
		Args:    cobra.ExactValidArgs(1),
		ValidArgs: []string{
			"all",
			"server",
			"peer",
			"client",
		},
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var names []string
			if opts.AltNames != "" {
				names = strings.Split(opts.AltNames, ",")
			}
			ca, err := manager.LoadCertificateAuthority(filepath.Join(opts.CertDir, "ca.crt"), filepath.Join(opts.CertDir, "ca.key"), names...)
			if err != nil {
				return err
			}
			switch args[0] {
			case "all":
				if err := ca.WriteAll(); err != nil {
					return err
				}
				log.Info("generated all certificates successfully.")
				return nil
			case "server":
				if err := ca.WriteServerCertAndKey(); err != nil {
					return err
				}
				log.Info("generated server certificates successfully.")
				return nil
			case "peer":
				if err := ca.WritePeerCertAndKey(); err != nil {
					return err
				}
				log.Info("generated peer certificates successfully.")
				return nil
			case "client":
				if err := ca.WriteClientCertAndKey(); err != nil {
					return err
				}
				log.Info("generated client certificates successfully.")
				return nil
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.CertDir, "cert-dir", "", "")
	cmd.Flags().StringVar(&opts.AltNames, "alt-names", "", "")
	return cmd
}
