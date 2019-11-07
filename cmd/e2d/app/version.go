package app

import (
	"encoding/json"
	"fmt"

	"github.com/criticalstack/e2d/pkg/buildinfo"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/spf13/cobra"
	"go.etcd.io/etcd/version"
)

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "etcd version",
		Run: func(cmd *cobra.Command, args []string) {
			data, err := json.Marshal(map[string]map[string]string{
				"etcd": {
					"Version": version.Version,
				},
				"e2d": {
					"Version": buildinfo.Version,
					"GitSHA":  buildinfo.GitSHA,
				},
			})
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("%s\n", data)
		},
	}
	return cmd
}
