package app

import (
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"

	"github.com/criticalstack/e2d/cmd/e2d/app/certs"
	"github.com/criticalstack/e2d/cmd/e2d/app/run"
	"github.com/criticalstack/e2d/cmd/e2d/app/version"
	"github.com/criticalstack/e2d/pkg/log"
)

var opts struct {
	Verbose bool
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "e2d",
		Short: "etcd manager",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if opts.Verbose {
				log.SetLevel(zapcore.DebugLevel)
			}
		},
	}

	cmd.AddCommand(
		newCompletionCmd(cmd),
		certs.NewCommand(),
		run.NewCommand(),
		version.NewCommand(),
	)

	cmd.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false, "verbose log output (debug)")
	return cmd
}

func newCompletionCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generates bash completion scripts",
		Run: func(cmd *cobra.Command, args []string) {
			w := os.Stdout
			if len(args) > 0 {
				var err error
				w, err = os.OpenFile(args[0], os.O_RDWR|os.O_CREATE, 0644)
				if err != nil {
					log.Fatal(err)
				}
				defer w.Close()
			}
			if err := rootCmd.GenBashCompletion(w); err != nil {
				log.Fatal(err)
			}
		},
	}
	return cmd
}
