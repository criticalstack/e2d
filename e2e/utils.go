//nolint
package e2e

import (
	"encoding/json"
	"strconv"

	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
)

func ParseAddr(s string) configv1alpha1.APIEndpoint {
	s = strconv.Quote(s)
	var v configv1alpha1.APIEndpoint
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(err)
	}
	return configv1alpha1.APIEndpoint{
		Host: v.Host,
		Port: v.Port,
	}
}
