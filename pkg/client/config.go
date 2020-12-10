//nolint:maligned
package client

import (
	"time"

	"go.etcd.io/etcd/pkg/transport"
)

type Config struct {
	ClientURLs     []string
	SecurityConfig SecurityConfig
	Timeout        time.Duration

	// NOTE: AutoSync sets client endpoints based upon the current members.
	// This can cause the endpoints to become unreachable if the members are
	// not directly accessible (e.g. a terminating load balancer). This is
	// disabled by default and can be enabled by passed a non-zero duration.
	AutoSyncInterval time.Duration
}

func (c *Config) validate() error {
	if c.Timeout == 0 {
		c.Timeout = 2 * time.Second
	}
	return nil
}

type SecurityConfig struct {
	CertFile      string
	KeyFile       string
	CertAuth      bool
	TrustedCAFile string
	AutoTLS       bool
}

func (sc SecurityConfig) Enabled() bool {
	return sc.CertFile != "" || sc.KeyFile != "" || sc.CertAuth || sc.TrustedCAFile != "" || sc.AutoTLS
}

func (sc SecurityConfig) Scheme() string {
	if sc.Enabled() {
		return "https"
	}
	return "http"
}

func (sc SecurityConfig) TLSInfo() transport.TLSInfo {
	return transport.TLSInfo{
		CertFile:       sc.CertFile,
		KeyFile:        sc.KeyFile,
		ClientCertAuth: sc.CertAuth,
		TrustedCAFile:  sc.TrustedCAFile,
	}
}
