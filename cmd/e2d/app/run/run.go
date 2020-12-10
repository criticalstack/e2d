package run

import (
	"encoding/json"
	"os"

	configutil "github.com/criticalstack/crit/pkg/config/util"
	"github.com/criticalstack/crit/pkg/kubernetes/yaml"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/manager"
)

var opts struct {
	ConfigFile string
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "run",
		Short:         "start a managed etcd instance",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := os.Stat(opts.ConfigFile); os.IsNotExist(err) {
				m, err := manager.New(&configv1alpha1.Configuration{})
				if err != nil {
					return err
				}
				return m.Run()
			}
			data, err := configutil.ReadFile(opts.ConfigFile)
			if err != nil {
				return err
			}
			data, err = injectVersion(data)
			if err != nil {
				return err
			}
			obj, err := yaml.UnmarshalFromYaml(data, configv1alpha1.SchemeGroupVersion)
			if err != nil {
				return err
			}
			cfg, ok := obj.(*configv1alpha1.Configuration)
			if !ok {
				return errors.Errorf("expected %q, received %T", configv1alpha1.SchemeGroupVersion, obj)
			}
			m, err := manager.New(cfg)
			if err != nil {
				return err
			}
			return m.Run()
		},
	}
	cmd.Flags().StringVarP(&opts.ConfigFile, "config", "c", "config.yaml", "config file")
	return cmd
}

func injectVersion(data []byte) ([]byte, error) {
	resources, err := yaml.UnmarshalFromYamlUnstructured(data)
	if err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return nil, errors.Errorf("cannot find resources in configuration file: %q", opts.ConfigFile)
	}
	u := resources[0]
	if u.GetAPIVersion() == "" {
		u.SetAPIVersion(configv1alpha1.SchemeGroupVersion.String())
	}
	if u.GetKind() == "" {
		u.SetKind("Configuration")
	}
	return json.Marshal(u)
}
