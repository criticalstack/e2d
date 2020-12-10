package version

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.etcd.io/etcd/version"

	"github.com/criticalstack/e2d/internal/buildinfo"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "version",
		Short:         "etcd version",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := json.Marshal(map[string]map[string]string{
				"etcd": {
					"Version": version.Version,
				},
				"e2d": {
					"Version": buildinfo.Version,
					"GitSHA":  buildinfo.GitSHA,
				},
				"build": {
					"Date":      buildinfo.Date,
					"GoVersion": buildinfo.GoVersion,
				},
			})
			if err != nil {
				return err
			}
			fmt.Printf("%s\n", data)
			return nil
		},
	}
	return cmd
}
