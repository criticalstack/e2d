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
	// TODO(chris): expose e2d version alongside of etcd version
	cmd := &cobra.Command{
		Use:   "version",
		Short: "etcd version",
		Run: func(cmd *cobra.Command, args []string) {
			data, err := json.Marshal(map[string]map[string]string{
				"etcd": map[string]string{
					"Version": version.Version,
				},
				"e2d": map[string]string{
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
