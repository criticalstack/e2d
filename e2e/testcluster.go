//nolint
package e2e

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

var (
	nextPort int32 = 2000

	testKey1   = "testkey1"
	testValue1 = "testvalue1"
)

type TestCluster struct {
	Name                string
	t                   *testing.T
	nodes               map[string]*Node
	requiredClusterSize int
	dir                 string
	cfg                 *configv1alpha1.Configuration
}

func NewTestCluster(t *testing.T, n int) *TestCluster {
	t.Parallel()
	h := sha256.Sum256([]byte(t.Name()))
	cfg := &configv1alpha1.Configuration{
		RequiredClusterSize: n,
		HealthCheckInterval: metav1.Duration{Duration: 1 * time.Second},
		// This setting is not set based upon a realistic time we expect
		// failure. It is set to be the lowest timeout it can be here to
		// test scenarios involving health check timeout while accounting
		// for the worst case scenario in performance of the test runner.
		// In other words, we don't want this to fail when we aren't
		// testing something like the removal of dead nodes just because
		// the system resources cause it to take too long to run the test.
		HealthCheckTimeout: metav1.Duration{Duration: 15 * time.Second},
		EtcdLogLevel:       "info",
		MemberlistLogLevel: "debug",
	}
	return &TestCluster{
		Name:                fmt.Sprintf("%x", h[:5]),
		t:                   t,
		nodes:               make(map[string]*Node),
		requiredClusterSize: n,
		dir:                 filepath.Join("testdata", fmt.Sprintf("%x", h[:5])),
		cfg:                 cfg,
	}
}

func (c *TestCluster) Setup() *TestCluster {
	if err := os.RemoveAll(c.dir); err != nil {
		c.t.Fatal(err)
	}
	return c
}

func (c *TestCluster) WriteCerts() *TestCluster {
	if err := manager.WriteNewCA(c.dir); err != nil {
		c.t.Fatal(err)
	}
	hostIP, err := netutil.DetectHostIPv4()
	if err != nil {
		c.t.Fatal(err)
	}
	ca, err := manager.LoadCertificateAuthority(filepath.Join(c.dir, "ca.crt"), filepath.Join(c.dir, "ca.key"), hostIP)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := ca.WriteAll(); err != nil {
		c.t.Fatal(err)
	}
	return c
}

func (c *TestCluster) Cleanup() {
	for _, node := range c.nodes {
		node.HardStop()
	}
}

func (c *TestCluster) Node(name string) *Node {
	node, ok := c.nodes[name]
	if !ok {
		c.t.Fatalf("node not found: %#v", name)
	}
	return node
}

func (c *TestCluster) Nodes(names ...string) *Context {
	nodes := make([]*Node, 0)
	for _, name := range names {
		nodes = append(nodes, c.Node(name))
	}
	return &Context{c: c, nodes: nodes}
}

func (c *TestCluster) All() *Context {
	nodes := make([]*Node, 0)
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	return &Context{c: c, nodes: nodes}
}

func (c *TestCluster) Leader() *Node {
	for _, node := range c.nodes {
		if node.Etcd().IsLeader() {
			return node
		}
	}
	return nil
}

func (c *TestCluster) Follower() *Node {
	for _, node := range c.nodes {
		if !node.Etcd().IsLeader() {
			return node
		}
	}
	return nil
}

func (c *TestCluster) IsHealthy() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, n := range c.nodes {
				cl := n.Client()
				if err := cl.IsHealthy(ctx); err != nil {
					log.Debug("node failed health check", zap.String("name", n.Name()), zap.Error(err))
					continue
				}
			}
			return nil
		case <-ctx.Done():
			return errors.Errorf("cluster %q unhealthy", c.Name)
		}
	}
}

func (c *TestCluster) getStarted() []string {
	nodes := make([]string, 0)
	for _, n := range c.nodes {
		if n.started {
			nodes = append(nodes, n.Etcd().Name)
		}
	}
	return nodes
}

func (c *TestCluster) WithConfig(fn func(*configv1alpha1.Configuration)) *TestCluster {
	fn(c.cfg)
	return c
}

func (c *TestCluster) AddNodes(names ...string) *Context {
	selected := make([]*Node, 0)
	for i := 0; i < len(names); i++ {
		cfg := c.cfg.DeepCopy()
		cfg.OverrideName = names[i]
		cfg.DataDir = filepath.Join(c.dir, cfg.OverrideName)
		if cfg.ClientAddr.IsZero() {
			cfg.ClientAddr.Port = atomic.AddInt32(&nextPort, 1)
		}
		if cfg.PeerAddr.IsZero() {
			cfg.PeerAddr.Port = atomic.AddInt32(&nextPort, 1)
		}
		if cfg.GossipAddr.IsZero() {
			cfg.GossipAddr.Host = "127.0.0.1"
			cfg.GossipAddr.Port = atomic.AddInt32(&nextPort, 1)
		}
		// This is a placeholder and will be replaced below after all nodes
		// have been added. It has to be here to ensure that the config
		// validation doesn't error when constructing a new manager.
		cfg.DiscoveryConfiguration.InitialPeers = []string{fmt.Sprintf(":%d", cfg.GossipAddr.Port)}
		m, err := manager.New(cfg)
		if err != nil {
			c.t.Fatal(err)
		}
		n := &Node{Manager: m, c: c}
		c.nodes[cfg.OverrideName] = n
		selected = append(selected, n)
	}

	// Here we ensure the initial peers are optimal when adding new nodes. This
	// helps avoid a situation where the node joins the gossip network with
	// only itself, and does not perform an initial push/pull, exchanging node
	// status information. When this happens it takes a full push/pull interval
	// (usually 30s) to exchange this information, slowing the test down.
	nodes := make([]*Node, 0)
	for _, n := range c.nodes {
		if !n.removed {
			nodes = append(nodes, n)
		}
	}
	for i := range nodes {
		nodes[i].Config().DiscoveryConfiguration.InitialPeers = []string{nodes[(i+1)%len(nodes)].Config().GossipAddr.String()}
	}
	return &Context{c: c, nodes: selected}
}
