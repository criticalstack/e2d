package app

import (
	"os"

	"github.com/criticalstack/e2d/pkg/log"
	"github.com/spf13/cobra"
)

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
			rootCmd.GenBashCompletion(w)
		},
	}
	return cmd
}
