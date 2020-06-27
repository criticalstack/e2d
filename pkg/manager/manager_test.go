//nolint:goconst
package manager

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cfssl/csr"
	"go.uber.org/zap/zapcore"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/netutil"
	"github.com/criticalstack/e2d/pkg/pki"
	"github.com/criticalstack/e2d/pkg/snapshot"
	snapshotutil "github.com/criticalstack/e2d/pkg/snapshot/util"
)

func writeFile(filename string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(filename, data, perm)
}

type testCluster struct {
	t     *testing.T
	nodes map[string]*Manager
}

func newTestCluster(t *testing.T) *testCluster {
	return &testCluster{t: t, nodes: make(map[string]*Manager)}
}

func (n *testCluster) addNode(name string, cfg *Config) {
	cfg.Name = name
	cfg.Dir = filepath.Join("testdata", name)
	m, err := New(cfg)
	if err != nil {
		n.t.Fatal(err)
	}
	n.nodes[name] = m
}

func (n *testCluster) lookupNode(name string) *Manager {
	node, ok := n.nodes[name]
	if !ok {
		n.t.Fatalf("node not found: %#v", name)
	}
	return node
}

func (n *testCluster) start(names ...string) {
	log.Infof("starting the following nodes: %v\n", names)
	for _, name := range names {
		go func(name string) {
			if err := n.lookupNode(name).Run(); err != nil {
				n.t.Fatal(err)
			}
		}(name)
	}
}

func (n *testCluster) restart(names ...string) {
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			if err := n.lookupNode(name).Restart(); err != nil {
				n.t.Fatal(err)
			}
		}(name)
	}
	wg.Wait()
}

func (n *testCluster) startAll() {
	for k := range n.nodes {
		n.start(k)
	}
}

func (n *testCluster) saveSnapshot(name string) {
	node := n.lookupNode(name)
	data, size, _, err := node.etcd.createSnapshot(0)
	if err != nil {
		n.t.Fatal(err)
	}
	if node.cfg.SnapshotEncryption {
		data = snapshotutil.NewEncrypterReadCloser(data, node.cfg.snapshotEncryptionKey, size)
	}
	if node.cfg.SnapshotCompression {
		data = snapshotutil.NewGzipReadCloser(data)
	}
	if err := node.snapshotter.Save(data); err != nil {
		n.t.Fatal(err)
	}
}

func (n *testCluster) stop(name string) {
	log.Infof("stopping node: %#v\n", name)
	n.lookupNode(name).HardStop()
}

func (n *testCluster) wait(names ...string) {
	log.Infof("waiting for the following nodes to be running: %v\n", names)
	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			for {
				if n.lookupNode(name).etcd.isRunning() {
					return
				}
				time.Sleep(100 * time.Millisecond)
			}
		}(name)
	}
	wg.Wait()
}

func (n *testCluster) waitRemoved(removed string, nodes ...string) {
	log.Infof("waiting for the node %#v to be removed from the following nodes: %v\n", removed, nodes)
	var wg sync.WaitGroup
	for _, name := range nodes {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			for removedNode := range n.lookupNode(name).removeCh {
				if removedNode == removed {
					return
				}
			}
		}(name)
	}
	wg.Wait()
}

func (n *testCluster) leader() *Manager {
	for _, node := range n.nodes {
		if node.etcd.isLeader() {
			return node
		}
	}
	return nil
}

func (n *testCluster) cleanup() {
	for _, node := range n.nodes {
		node.HardStop()
	}
}

func newTestClient(addr string) *Client {
	caddr, _ := netutil.ParseAddr(addr)
	if caddr.Port == 0 {
		caddr.Port = 2379
	}
	clientURL := url.URL{Scheme: "http", Host: caddr.String()}
	c, err := newClient(&client.Config{
		ClientURLs: []string{clientURL.String()},
		Timeout:    5 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	return c
}

func newSecureTestClient(addr, certFile, clientCertFile, clientKeyFile string) *Client {
	caddr, _ := netutil.ParseAddr(addr)
	if caddr.Port == 0 {
		caddr.Port = 2379
	}
	clientURL := url.URL{Scheme: "https", Host: caddr.String()}
	c, err := newClient(&client.Config{
		ClientURLs: []string{clientURL.String()},
		SecurityConfig: client.SecurityConfig{
			CertFile:      clientCertFile,
			KeyFile:       clientKeyFile,
			CertAuth:      true,
			TrustedCAFile: certFile,
		},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		panic(err)
	}
	return c
}

func newFileSnapshotter(path string) *snapshot.FileSnapshotter {
	s, _ := snapshot.NewFileSnapshotter(path)
	return s
}

var testLong = flag.Bool("test.long", false, "enable running larger tests")

func init() {
	for _, arg := range os.Args[1:] {
		if arg == "-test.long" {
			*testLong = true
		}
	}
	log.SetLevel(zapcore.DebugLevel)
}

// TODO(chris): a lot of cases here create a healthy 3 node cluster, so create
// a function to do that to make the test code more succinct

func TestManagerSingleFaultRecovery(t *testing.T) {
	// TODO(chris): break this test into two version, one where the leader dies
	// and one where a follower dies
	if !*testLong {
		t.Skip()
	}

	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	})

	c.startAll()
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newTestClient(":2379")
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}

	c.stop("node1")
	c.waitRemoved("node1", "node2", "node3")
	c.addNode("node4", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
	})
	c.start("node4")
	c.wait("node2", "node3", "node4")
	fmt.Println("healthy!")
	cl = newTestClient(":2379")
	v, err = cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func TestManagerRestoreClusterFromSnapshotNoCompression(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})

	c.startAll()
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newTestClient(":2479")
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
	leader := c.leader().cfg.Name
	fmt.Printf("leader = %+v\n", leader)
	c.saveSnapshot(leader)
	c.stop("node1")
	c.stop("node2")
	c.stop("node3")

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.addNode("node4", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})
	c.addNode("node5", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})
	c.addNode("node6", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})
	c.start("node4", "node5", "node6")
	c.wait("node4", "node5", "node6")
	cl = newTestClient(":2379")
	v, err = cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func TestManagerRestoreClusterFromSnapshotCompression(t *testing.T) {
	if !*testLong {
		t.Skip()
	}

	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
	})

	c.startAll()
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newTestClient(":2479")
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
	leader := c.leader().cfg.Name
	fmt.Printf("leader = %+v\n", leader)
	c.saveSnapshot(leader)
	c.stop("node1")
	c.stop("node2")
	c.stop("node3")

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.addNode("node4", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
	})
	c.addNode("node5", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
	})
	c.addNode("node6", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
	})
	c.start("node4", "node5", "node6")
	c.wait("node4", "node5", "node6")
	cl = newTestClient(":2379")
	v, err = cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func TestManagerRestoreClusterFromSnapshotEncryption(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	if err := writeTestingCerts(); err != nil {
		t.Fatal(err)
	}

	caCertFile := "testdata/ca.crt"
	caKeyFile := "testdata/ca.key"
	serverCertFile := "testdata/server.crt"
	serverKeyFile := "testdata/server.key"
	peerCertFile := "testdata/peer.crt"
	peerKeyFile := "testdata/peer.key"
	clientCertFile := "testdata/client.crt"
	clientKeyFile := "testdata/client.key"

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
		SnapshotEncryption:  true,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
		SnapshotEncryption:  true,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
		SnapshotEncryption:  true,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})

	c.startAll()
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newSecureTestClient(":2479", caCertFile, clientCertFile, clientKeyFile)
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
	leader := c.leader().cfg.Name
	fmt.Printf("leader = %+v\n", leader)
	c.saveSnapshot(leader)
	c.stop("node1")
	c.stop("node2")
	c.stop("node3")

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.addNode("node4", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node5", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node6", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
		SnapshotCompression: true,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.start("node4", "node5", "node6")
	c.wait("node4", "node5", "node6")
	cl = newSecureTestClient(":2379", caCertFile, clientCertFile, clientKeyFile)
	v, err = cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func TestManagerSingleNodeRestart(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})

	c.startAll()
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newTestClient(":2379")
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}

	c.stop("node1")

	// The important part for this test to work is that the cluster cannot
	// remove node1, which is why the HealthCheckInterval has been increased
	// and we are not waiting for the node to be removed. The existing node is
	// started again after being stopped so it should use the same data-dir.
	c.start("node1")
	c.wait("node1", "node2", "node3")
	fmt.Println("healthy!")

	// It is possible that the client might fail here because the new member
	// takes longer than usual to respond. The timeout has been increased in
	// the test client in response to this, but this may need to be
	// reevaluated.
	cl = newTestClient(":2379")
	v, err = cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func TestManagerNodeReplacementUsedPeerAddr(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})

	c.startAll()
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newTestClient(":2379")
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}

	// Impersonate node1 and replace it with node4 which happens to have the
	// same inet. It's expected that node1 will be removed from the cluster
	// during node4 join because it's not allowed to join a new node that uses
	// an existing peerAddr (and having two nodes with the same peerAddr is
	// impossible since the peerAddr must be a routable IP:port).
	c.addNode("node4", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7983",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  15 * time.Second,
	})

	c.stop("node1")
	c.start("node4")

	// node1 should no longer be present in the etcd membership
	c.waitRemoved("node1", "node2", "node3")

	// cluster should be up with node4
	waitChan := make(chan struct{})

	go func() {
		c.wait("node2", "node3", "node4")
		waitChan <- struct{}{}
	}()

	select {
	case <-waitChan:
		break
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for node4 to become healthy")
	}

	fmt.Println("healthy!")

	// It is possible that the client might fail here because the new member
	// takes longer than usual to respond. The timeout has been increased in
	// the test client in response to this, but this may need to be
	// reevaluated.
	cl = newTestClient(":2379")
	v, err = cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func writeTestingCerts() error {
	r, err := pki.NewDefaultRootCA()
	if err != nil {
		return err
	}
	if err := writeFile("testdata/ca.crt", r.CA.CertPEM, 0644); err != nil {
		return err
	}
	if err := writeFile("testdata/ca.key", r.CA.KeyPEM, 0600); err != nil {
		return err
	}
	certs, err := r.GenerateCertificates(pki.ServerSigningProfile, &csr.CertificateRequest{
		Names: []csr.Name{
			{
				C:  "US",
				ST: "Boston",
				L:  "MA",
			},
		},
		KeyRequest: &csr.KeyRequest{
			A: "rsa",
			S: 2048,
		},
		Hosts: []string{"127.0.0.1"},
		CN:    "etcd server",
	})
	if err != nil {
		return err
	}

	if err := writeFile("testdata/server.crt", certs.CertPEM, 0644); err != nil {
		return err
	}
	if err := writeFile("testdata/server.key", certs.KeyPEM, 0600); err != nil {
		return err
	}
	certs, err = r.GenerateCertificates(pki.PeerSigningProfile, &csr.CertificateRequest{
		Names: []csr.Name{
			{
				C:  "US",
				ST: "Boston",
				L:  "MA",
			},
		},
		KeyRequest: &csr.KeyRequest{
			A: "rsa",
			S: 2048,
		},
		Hosts: []string{"127.0.0.1"},
		CN:    "etcd peer",
	})
	if err != nil {
		return err
	}

	if err := writeFile("testdata/peer.crt", certs.CertPEM, 0644); err != nil {
		return err
	}
	if err := writeFile("testdata/peer.key", certs.KeyPEM, 0600); err != nil {
		return err
	}
	certs, err = r.GenerateCertificates(pki.ClientSigningProfile, &csr.CertificateRequest{
		Names: []csr.Name{
			{
				C:  "US",
				ST: "Boston",
				L:  "MA",
			},
		},
		KeyRequest: &csr.KeyRequest{
			A: "rsa",
			S: 2048,
		},
		Hosts: []string{""},
		CN:    "etcd client",
	})
	if err != nil {
		return err
	}

	if err := writeFile("testdata/client.crt", certs.CertPEM, 0644); err != nil {
		return err
	}
	if err := writeFile("testdata/client.key", certs.KeyPEM, 0600); err != nil {
		return err
	}
	return nil
}

func TestManagerSecurityConfig(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	if err := writeTestingCerts(); err != nil {
		t.Fatal(err)
	}

	caCertFile := "testdata/ca.crt"
	caKeyFile := "testdata/ca.key"
	serverCertFile := "testdata/server.crt"
	serverKeyFile := "testdata/server.key"
	peerCertFile := "testdata/peer.crt"
	peerKeyFile := "testdata/peer.key"
	clientCertFile := "testdata/client.crt"
	clientKeyFile := "testdata/client.key"

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})

	c.start("node1", "node2", "node3")
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newSecureTestClient(":2379", caCertFile, clientCertFile, clientKeyFile)
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	cl.Close()
	cl = newSecureTestClient(":2479", caCertFile, clientCertFile, clientKeyFile)
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}

func TestManagerReadExistingName(t *testing.T) {
	if !*testLong {
		t.Skip()
	}

	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()
	name := "node1"

	var err error
	c.nodes[name], err = New(&Config{
		Name:                name,
		Dir:                 filepath.Join("testdata", name),
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 1,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.startAll()
	c.wait("node1")

	fmt.Println("ready")
	cl := newTestClient(":2379")
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}

	c.stop(name)
	newNode, err := New(&Config{
		Dir:                 filepath.Join("testdata", name),
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 1,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if newNode.cfg.Name != name {
		t.Fatalf("expected %#v, received %#v", name, newNode.cfg.Name)
	}
}

func TestManagerDeleteVolatile(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 1,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})

	c.startAll()
	c.wait("node1")
	fmt.Println("ready")
	cl := newTestClient(":2379")
	volatilePrefix := "/_e2d"
	nkeys := 10
	for i := 0; i < nkeys; i++ {
		if err := cl.Set(fmt.Sprintf("%s/%d", volatilePrefix, i), "testvalue1"); err != nil {
			t.Fatal(err)
		}
	}
	n, err := cl.Count(volatilePrefix)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()

	// cluster-info is added by e2db which contains a key with the cluster-info
	// itself, and another key for the e2db table schema
	if n != int64(nkeys+2) {
		t.Fatalf("expected %d keys, received %d", nkeys+2, n)
	}
	c.saveSnapshot("node1")
	c.stop("node1")

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.addNode("node2", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 1,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
		Snapshotter:         newFileSnapshotter("testdata/snapshots"),
	})
	c.start("node2")
	c.wait("node2")
	cl = newTestClient(":2379")
	_, err = cl.Get("/_e2d/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	n, err = cl.Count("/_e2d")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after snapshot recover, only 1 key/value should remain, received %d", n)
	}
	cl.Close()
}

func TestManagerServerRestartCertRenewal(t *testing.T) {
	if !*testLong {
		t.Skip()
	}
	if err := os.RemoveAll("testdata"); err != nil {
		t.Fatal(err)
	}

	if err := writeTestingCerts(); err != nil {
		t.Fatal(err)
	}

	caCertFile := "testdata/ca.crt"
	caKeyFile := "testdata/ca.key"
	serverCertFile := "testdata/server.crt"
	serverKeyFile := "testdata/server.key"
	peerCertFile := "testdata/peer.crt"
	peerKeyFile := "testdata/peer.key"
	clientCertFile := "testdata/client.crt"
	clientKeyFile := "testdata/client.key"

	c := newTestCluster(t)
	defer c.cleanup()

	c.addNode("node1", &Config{
		ClientAddr:          ":2379",
		PeerAddr:            ":2380",
		GossipAddr:          ":7980",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node2", &Config{
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7981",
		BootstrapAddrs:      []string{":7980"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})
	c.addNode("node3", &Config{
		ClientAddr:          ":2579",
		PeerAddr:            ":2580",
		GossipAddr:          ":7982",
		BootstrapAddrs:      []string{":7981"},
		RequiredClusterSize: 3,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		ClientSecurity: client.SecurityConfig{
			CertFile:      serverCertFile,
			KeyFile:       serverKeyFile,
			TrustedCAFile: caCertFile,
		},
		PeerSecurity: client.SecurityConfig{
			CertFile:      peerCertFile,
			KeyFile:       peerKeyFile,
			TrustedCAFile: caCertFile,
		},
		CACertFile: caCertFile,
		CAKeyFile:  caKeyFile,
	})

	c.start("node1", "node2", "node3")
	c.wait("node1", "node2", "node3")
	fmt.Println("ready")
	cl := newSecureTestClient(":2379", caCertFile, clientCertFile, clientKeyFile)
	testKey1 := "testkey1"
	testValue1 := "testvalue1"
	if err := cl.Set(testKey1, testValue1); err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if err := writeTestingCerts(); err != nil {
		t.Fatal(err)
	}
	c.restart("node1", "node2", "node3")
	cl = newSecureTestClient(":2479", caCertFile, clientCertFile, clientKeyFile)
	v, err := cl.Get(testKey1)
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	if string(v) != testValue1 {
		t.Fatalf("expected %#v, received %#v", testValue1, string(v))
	}
}
