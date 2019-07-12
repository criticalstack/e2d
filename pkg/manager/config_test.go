package manager

import (
	"fmt"
	"testing"

	"github.com/criticalstack/e2d/pkg/netutil"
)

func TestConfigUnspecifiedAddr(t *testing.T) {
	host, err := netutil.DetectHostIPv4()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		ClientAddr:     "0.0.0.0:2379",
		PeerAddr:       "0.0.0.0:2380",
		GossipAddr:     "0.0.0.0:7980",
		BootstrapAddrs: []string{"0.0.0.0:7981"},
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.ClientAddr != fmt.Sprintf("%s:%d", host, 2379) {
		t.Fatalf("ClientAddr unspecified address not fixed: %v", cfg.ClientAddr)
	}
	if cfg.PeerAddr != fmt.Sprintf("%s:%d", host, 2380) {
		t.Fatalf("PeerAddr unspecified address not fixed: %v", cfg.PeerAddr)
	}
	if cfg.GossipAddr != fmt.Sprintf("%s:%d", host, 7980) {
		t.Fatalf("GossipAddr unspecified address not fixed: %v", cfg.GossipAddr)
	}
	if cfg.BootstrapAddrs[0] != fmt.Sprintf("%s:%d", host, 7981) {
		t.Fatalf("BootstrapAddr unspecified address not fixed: %v", cfg.BootstrapAddrs[0])
	}
}
