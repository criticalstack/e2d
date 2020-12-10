//nolint:goconst
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/etcdserver"
	"github.com/criticalstack/e2d/pkg/log"
)

func init() {
	log.SetLevel(zapcore.DebugLevel)
}

func TestManagerSingleFaultRecoveryFollower(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1")
	c.Follower().
		Stop().Remove().Wait()
	c.AddNodes("node4").
		Start().Wait().
		TestClientGet("node4")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerSingleFaultRecoveryLeader(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1")
	c.Leader().
		Stop().Remove().Wait()
	c.AddNodes("node4").
		Start().Wait().
		TestClientGet("node4")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerRestoreClusterFromSnapshotNoCompression(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.SnapshotConfiguration.File = "file://" + filepath.Join(c.dir, "snapshots")
	})
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node2")
	c.Leader().
		SaveSnapshot()
	c.Nodes("node1", "node2", "node3").
		Stop().Remove()

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.AddNodes("node4", "node5", "node6").
		Start().Wait().
		TestClientGet("node4")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerRestoreClusterFromSnapshotCompression(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.SnapshotConfiguration.Compression = true
		cfg.SnapshotConfiguration.File = "file://" + filepath.Join(c.dir, "snapshots")
	})
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node2")
	c.Leader().
		SaveSnapshot()
	c.Nodes("node1", "node2", "node3").
		Stop().Remove()

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.SnapshotConfiguration.Compression = false
	}).AddNodes("node4", "node5", "node6").
		Start().Wait().
		TestClientGet("node4")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerRestoreClusterFromSnapshotEncryption(t *testing.T) {
	c := NewTestCluster(t, 3).Setup().WriteCerts()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.SnapshotConfiguration.Compression = true
		cfg.SnapshotConfiguration.Encryption = true
		cfg.SnapshotConfiguration.File = "file://" + filepath.Join(c.dir, "snapshots")
		cfg.CACert = filepath.Join(c.dir, "ca.crt")
		cfg.CAKey = filepath.Join(c.dir, "ca.key")
	})
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node2")
	c.Leader().
		SaveSnapshot()
	c.Nodes("node1", "node2", "node3").
		Stop().Remove()

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.SnapshotConfiguration.Encryption = false
	}).AddNodes("node4", "node5", "node6").
		Start().Wait().
		TestClientGet("node4")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerSingleNodeRestart(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1")

	// The important part for this test to work is that the cluster cannot
	// remove node1, which is why we are not waiting for the node to be
	// removed. The existing node is started again after being stopped so it
	// should use the same data-dir.
	c.Nodes("node1").
		Stop().Start().Wait().
		TestClientGet("node1")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerNodeReplacementUsedPeerAddr(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1")

	// Impersonate node1 and replace it with node4 which happens to have the
	// same inet. It's expected that node1 will be removed from the cluster
	// during node4 join because it's not allowed to join a new node that uses
	// an existing peerAddr (and having two nodes with the same peerAddr is
	// impossible since the peerAddr must be a routable IP:port).
	c.Nodes("node1").
		Stop().Remove().Wait()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.ClientAddr = c.Node("node1").Config().ClientAddr
		cfg.PeerAddr = c.Node("node1").Config().PeerAddr
		cfg.GossipAddr = c.Node("node1").Config().GossipAddr
	}).AddNodes("node4").
		Start().Wait().
		TestClientGet("node4")
}

func TestManagerSecurityConfig(t *testing.T) {
	c := NewTestCluster(t, 3).Setup().WriteCerts()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.CACert = filepath.Join(c.dir, "ca.crt")
		cfg.CAKey = filepath.Join(c.dir, "ca.key")
	})
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1").
		TestClientGet("node2")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerReadExistingName(t *testing.T) {
	c := NewTestCluster(t, 1).Setup()
	defer c.Cleanup()

	name := "node1"

	c.AddNodes(name).
		Start().Wait().
		TestClientSet("node1").
		Stop()
	c.AddNodes(name).
		Start().Wait()

	if c.Node(name).Name() != name {
		t.Fatalf("expected %#v, received %#v", name, c.Node(name).Etcd().Name)
	}

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerDeleteVolatile(t *testing.T) {
	c := NewTestCluster(t, 1).Setup()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.SnapshotConfiguration.File = "file://" + filepath.Join(c.dir, "snapshots")
	})
	defer c.Cleanup()

	c.AddNodes("node1").
		Start().Wait()
	cl := c.Node("node1").Client()
	nkeys := 10
	for i := 0; i < nkeys; i++ {
		if err := cl.Set(fmt.Sprintf("%s/%d", etcdserver.VolatilePrefix, i), "testvalue1"); err != nil {
			t.Fatal(err)
		}
	}
	n, err := cl.Count(string(etcdserver.VolatilePrefix))
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()

	// cluster-info is added by e2db which contains a key with the cluster-info
	// itself, and another key for the e2db table schema
	if n != int64(nkeys+2) {
		t.Fatalf("expected %d keys, received %d", nkeys+2, n)
	}
	c.Nodes("node1").
		SaveSnapshot()
	c.Nodes("node1").Stop().Remove()

	// need to wait a bit to ensure the port is free to bind
	time.Sleep(1 * time.Second)

	// SnapshotInterval is 0 so creating snapshots is disabled, however,
	// SnapshotDir is being replaced with default SnapshotDir from node1 so
	// that this new node can restore that snapshot
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.ClientAddr = c.Node("node1").Config().ClientAddr
		cfg.PeerAddr = c.Node("node1").Config().PeerAddr
		cfg.GossipAddr = c.Node("node1").Config().GossipAddr
	}).AddNodes("node2").
		Start().Wait()
	cl = c.Node("node2").Client()

	// There is a short race after etcdserver is ready and the manager places
	// the snapshot marker. This will be fixed in the future, for now just make
	// sure the marker gets placed before stopping the node.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cl.MustGet(ctx, string(etcdserver.SnapshotMarkerKey)); err != nil {
		t.Fatal(err)
	}

	n, err = cl.Count(string(etcdserver.VolatilePrefix))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after snapshot recover, only 1 key/value should remain, received %d", n)
	}
	cl.Close()

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerServerRestartCertRenewal(t *testing.T) {
	c := NewTestCluster(t, 3).Setup().WriteCerts()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.CACert = filepath.Join(c.dir, "ca.crt")
		cfg.CAKey = filepath.Join(c.dir, "ca.key")
	})
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1")

	// replace certs on disk
	c.WriteCerts()
	c.All().Restart().Wait().
		TestClientGet("node2")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerRollingUpdate(t *testing.T) {
	c := NewTestCluster(t, 3).Setup()
	defer c.Cleanup()

	c.AddNodes("node1", "node2", "node3").
		Start().Wait().
		TestClientSet("node1")
	c.AddNodes("node4").
		Start().Wait()
	c.Nodes("node1").
		Stop().Remove().Wait().
		TestClientGet("node4")

	log.Info("test completed successfully", zap.String("test", t.Name()))
}

func TestManagerMetricsDisableAuth(t *testing.T) {
	c := NewTestCluster(t, 1).Setup()
	c.WithConfig(func(cfg *configv1alpha1.Configuration) {
		cfg.MetricsConfiguration.Addr = ParseAddr(":4001")
		cfg.MetricsConfiguration.DisableAuth = true
	})
	defer c.Cleanup()

	c.AddNodes("node1").
		Start().Wait()

	resp, err := http.Get("http://127.0.0.1:4001/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected status code 200, received %d", resp.StatusCode)
	}

	log.Info("test completed successfully", zap.String("test", t.Name()))
}
