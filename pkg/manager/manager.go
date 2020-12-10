package manager

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"

	"github.com/criticalstack/e2d/pkg/client"
	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/discovery"
	"github.com/criticalstack/e2d/pkg/etcdserver"
	"github.com/criticalstack/e2d/pkg/gossip"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager/e2dpb"
	"github.com/criticalstack/e2d/pkg/snapshot"
	snapshotutil "github.com/criticalstack/e2d/pkg/snapshot/util"
)

// Manager manages an embedded etcd instance.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg    *configv1alpha1.Configuration
	etcd   *etcdserver.Server
	gossip *gossip.Gossip

	peerGetter            discovery.PeerGetter
	snapshotter           snapshot.Snapshotter
	snapshotEncryptionKey *[32]byte

	removeCh chan string
}

// New creates a new instance of Manager.
func New(cfg *configv1alpha1.Configuration) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, errors.Wrap(err, "e2d config is invalid")
	}

	var clientSecurity, peerSecurity client.SecurityConfig
	if cfg.CACert != "" && cfg.CAKey != "" {
		ca, err := LoadCertificateAuthority(cfg.CACert, cfg.CAKey)
		if err != nil {
			return nil, err
		}
		if err := ca.WriteAll(); err != nil {
			return nil, err
		}
		dir := filepath.Dir(cfg.CACert)
		clientSecurity = client.SecurityConfig{
			CertFile:      filepath.Join(dir, "server.crt"),
			KeyFile:       filepath.Join(dir, "server.key"),
			TrustedCAFile: cfg.CACert,
		}
		peerSecurity = client.SecurityConfig{
			CertFile:      filepath.Join(dir, "peer.crt"),
			KeyFile:       filepath.Join(dir, "peer.key"),
			TrustedCAFile: cfg.CACert,
		}
	}
	clientURL := url.URL{Scheme: clientSecurity.Scheme(), Host: cfg.ClientAddr.String()}
	peerURL := url.URL{Scheme: peerSecurity.Scheme(), Host: cfg.PeerAddr.String()}

	name, err := getExistingNameFromDataDir(filepath.Join(cfg.DataDir, "member/snap/db"), peerURL)
	if err != nil {
		log.Debug("cannot read existing data-dir", zap.Error(err))
		name = fmt.Sprintf("%X", rand.Uint64())
	}
	if cfg.OverrideName != "" {
		name = cfg.OverrideName
	}

	peerGetter, err := cfg.DiscoveryConfiguration.Setup()
	if err != nil {
		return nil, err
	}

	// the initial peers will only be seeded when the user does not provide any
	// initial peers
	if cfg.RequiredClusterSize > 1 && len(cfg.DiscoveryConfiguration.InitialPeers) == 0 {
		addrs, err := peerGetter.GetAddrs(context.Background())
		if err != nil {
			return nil, err
		}
		log.Debugf("cloud provided addresses: %v", addrs)
		for _, addr := range addrs {
			cfg.DiscoveryConfiguration.InitialPeers = append(cfg.DiscoveryConfiguration.InitialPeers, fmt.Sprintf("%s:%d", addr, configv1alpha1.DefaultGossipPort))
		}
		log.Debugf("bootstrap addrs: %v", cfg.DiscoveryConfiguration.InitialPeers)
		if len(cfg.DiscoveryConfiguration.InitialPeers) == 0 {
			return nil, errors.Errorf("bootstrap addresses must be provided")
		}
	}

	snapshotter, err := getSnapshotProvider(cfg.SnapshotConfiguration)
	if err != nil {
		return nil, err
	}

	// both memberlist security and snapshot encryption are implicitly based
	// upon the CA key
	var key [32]byte
	if cfg.CAKey != "" {
		key, err = ReadEncryptionKey(cfg.CAKey)
		if err != nil {
			return nil, err
		}
	}

	var etcdLogLevel, memberlistLogLevel zapcore.Level
	if err := etcdLogLevel.Set(cfg.EtcdLogLevel); err != nil {
		return nil, err
	}
	if err := memberlistLogLevel.Set(cfg.MemberlistLogLevel); err != nil {
		return nil, err
	}

	m := &Manager{
		cfg: cfg,
		etcd: &etcdserver.Server{
			Name:                name,
			DataDir:             cfg.DataDir,
			ClientURL:           clientURL,
			PeerURL:             peerURL,
			RequiredClusterSize: cfg.RequiredClusterSize,
			ClientSecurity:      clientSecurity,
			PeerSecurity:        peerSecurity,
			LogLevel:            etcdLogLevel,
			EnableLocalListener: true,
			PreVote:             !cfg.DisablePreVote,
			UnsafeNoFsync:       cfg.UnsafeNoFsync,
		},
		gossip: gossip.New(&gossip.Config{
			Name:       name,
			ClientURL:  clientURL.String(),
			PeerURL:    peerURL.String(),
			GossipHost: cfg.GossipAddr.Host,
			GossipPort: int(cfg.GossipAddr.Port),
			SecretKey:  key[:],
			LogLevel:   memberlistLogLevel,
		}),
		snapshotEncryptionKey: &key,
		removeCh:              make(chan string, 10),
		peerGetter:            peerGetter,
		snapshotter:           snapshotter,
	}
	if cfg.CompactionInterval.Duration != 0 {
		m.etcd.CompactionInterval = cfg.CompactionInterval.Duration
	}
	if !cfg.MetricsConfiguration.Addr.IsZero() {
		metricsURL := &url.URL{Scheme: clientSecurity.Scheme(), Host: cfg.MetricsConfiguration.Addr.String()}
		if cfg.MetricsConfiguration.DisableAuth {
			metricsURL.Scheme = "http"
		}
		m.etcd.MetricsURL = metricsURL
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.etcd.ServiceRegister = func(s *grpc.Server) {
		e2dpb.RegisterManagerServer(s, &ManagerService{m})
	}
	return m, nil
}

func (m *Manager) Name() string {
	if m.etcd == nil {
		return ""
	}
	return m.etcd.Name
}

func (m *Manager) Etcd() *etcdserver.Server {
	if m.etcd == nil {
		return nil
	}
	return m.etcd
}

func (m *Manager) Gossip() *gossip.Gossip {
	if m.gossip == nil {
		return nil
	}
	return m.gossip
}

func (m *Manager) Config() *configv1alpha1.Configuration {
	return m.cfg
}

func (m *Manager) RemoveCh() <-chan string {
	return m.removeCh
}

func (m *Manager) Snapshotter() snapshot.Snapshotter {
	return m.snapshotter
}

// HardStop stops all services and cleans up the Manager state. Unlike
// GracefulStop, it does not attempt to gracefully shutdown etcd.
func (m *Manager) HardStop() {
	if m.removeCh != nil {
		close(m.removeCh)
		m.removeCh = nil
	}
	m.cancel()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	if m.etcd != nil {
		log.Debug("attempting hard stop of etcd server ...")
		m.etcd.HardStop()
		<-m.etcd.Server.StopNotify()
		log.Debug("etcd server stopped")
	}
	if err := m.gossip.Shutdown(); err != nil {
		log.Debug("gossip shutdown failed", zap.Error(err))
	}
}

// GracefulStop stops all services and cleans up the Manager state. It attempts
// to gracefully shutdown etcd by waiting for gRPC calls in-flight to finish.
func (m *Manager) GracefulStop() {
	if m.removeCh != nil {
		close(m.removeCh)
		m.removeCh = nil
	}
	m.cancel()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	log.Debug("attempting graceful stop of etcd server ...")
	m.etcd.GracefulStop()
	<-m.etcd.Server.StopNotify()
	log.Debug("etcd server stopped")
	if err := m.gossip.Shutdown(); err != nil {
		log.Debug("gossip shutdown failed", zap.Error(err))
	}
}

func (m *Manager) Restart() error {
	peers := make([]*etcdserver.Peer, 0)
	for _, member := range m.etcd.Etcd.Server.Cluster().Members() {
		if len(member.PeerURLs) == 0 {
			continue
		}
		peers = append(peers, &etcdserver.Peer{Name: member.Name, URL: member.PeerURLs[0]})
	}
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()

	return m.etcd.Restart(ctx, peers)
}

// Run starts and manages an etcd node based upon the provided configuration.
// In the case of a fault, or if the manager is otherwise stopped, this method
// exits.
func (m *Manager) Run() error {
	if m.etcd.IsRunning() {
		return errors.New("etcd is already running")
	}

	switch m.cfg.RequiredClusterSize {
	case 1:
		// a single-node etcd cluster does not require gossip or need to wait for
		// other members and therefore can start immediately
		if err := m.startEtcdCluster([]*etcdserver.Peer{{Name: m.etcd.Name, URL: m.etcd.PeerURL.String()}}); err != nil {
			return err
		}
	case 3, 5:
		// all multi-node clusters require the gossip network to be started
		if err := m.gossip.Start(m.ctx, m.cfg.DiscoveryConfiguration.InitialPeers); err != nil {
			return err
		}

		// a multi-node etcd cluster will either be created or an existing one will
		// be joined
		if err := m.startOrJoinEtcdCluster(); err != nil {
			return err
		}

		if err := m.gossip.Update(gossip.Running); err != nil {
			log.Debugf("[%v]: cannot update member metadata: %v", m.etcd.Name, err)
		}
	}

	// cluster is ready so start maintenance loops
	go m.runMembershipCleanup()
	go m.runSnapshotter()

	for {
		select {
		case <-m.etcd.Server.StopNotify():
			log.Info("etcd server stopping ...",
				zap.Stringer("id", m.etcd.Server.ID()),
				zap.String("name", m.etcd.Name),
			)
			if m.etcd.IsRestarting() {
				time.Sleep(1 * time.Second)
				continue
			}
			if m.cfg.RequiredClusterSize == 1 {
				return nil
			}
			if err := m.gossip.Update(gossip.Unknown); err != nil {
				log.Debugf("[%v]: cannot update member metadata: %v", m.etcd.Name, err)
			}
			return nil
		case err := <-m.etcd.Err():
			return err
		case <-m.ctx.Done():
			return nil
		}
	}
}

func (m *Manager) startOrJoinEtcdCluster() error {
	ctx, cancel := context.WithTimeout(m.ctx, m.cfg.DiscoveryConfiguration.BootstrapTimeout.Duration)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// first use peers to attempt joining an existing cluster
			for _, member := range m.gossip.Members() {
				if member.Name == m.etcd.Name {
					continue
				}
				log.Debugf("[%v]: gossip peer: %+v", shortName(m.etcd.Name), member)
				if member.Status != gossip.Running {
					log.Debugf("[%v]: cannot join peer %#v in current status: %s", shortName(m.etcd.Name), shortName(member.Name), member.Status)
					continue
				}
				if err := m.joinEtcdCluster(member.ClientURL); err != nil {
					log.Debugf("[%v]: cannot join node %#v: %v", shortName(m.etcd.Name), member.ClientURL, err)
					continue
				}
				log.Debug("joined an existing etcd cluster successfully")
				return nil
			}
			log.Debugf("[%v]: cluster currently has %d members", shortName(m.etcd.Name), len(m.gossip.Members()))
			if len(m.gossip.Members()) < m.cfg.RequiredClusterSize {
				continue
			}
			if err := m.gossip.Update(gossip.Pending); err != nil {
				log.Debugf("[%v]: cannot update member metadata: %v", shortName(m.etcd.Name), err)
			}

			// when enough members are reporting in as pending, it means that a
			// majority of members were unable to connect to an existing
			// cluster
			if len(m.gossip.PendingMembers()) < m.cfg.RequiredClusterSize {
				log.Debugf("[%v]: members pending: %d", shortName(m.etcd.Name), len(m.gossip.PendingMembers()))
				continue
			}
			peers := make([]*etcdserver.Peer, 0)
			for _, m := range m.gossip.Members() {
				peers = append(peers, &etcdserver.Peer{Name: m.Name, URL: m.PeerURL})
			}
			return m.startEtcdCluster(peers)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// startEtcdCluster starts a new etcd cluster with the provided peers. The list
// of peers provided must be inclusive of this prospective instance. An attempt
// is made to restore from a previous snapshot when one is available.
//
// When restoring from a snapshot, all volatile keys are deleted and a snapshot
// marker is created. This enables clients using e2d to coordinate their
// cluster, by conveying information about whether this is a brand new cluster
// or an existing cluster that recovered from total cluster failure.
func (m *Manager) startEtcdCluster(peers []*etcdserver.Peer) error {
	snapshot, err := m.restoreFromSnapshot(peers)
	if err != nil {
		log.Error("cannot restore snapshot", zap.Error(err))
	}
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()

	if err := m.etcd.StartNew(ctx, peers); err != nil {
		return err
	}
	if !snapshot {
		return nil
	}

	// These operations directly interact with the etcd key/value store,
	// therefore do NOT get committed through the raft log. This is OK
	// since all servers that recover from a snapshot will perform the same
	// operations and the outcome is deterministic.
	rev, deleted, err := m.etcd.ClearVolatilePrefix()
	if err != nil {
		if errors.Cause(err) != etcdserver.ErrServerStopped {
			return err
		}
		log.Debug("cannot clear volatile prefix", zap.Error(err))
		return nil
	}
	log.Debug("deleted volatile keys",
		zap.Int64("deleted-keys", deleted),
		zap.Int64("revision", rev),
	)
	v := []byte(time.Now().Format(time.RFC3339))
	rev, err = m.etcd.PlaceSnapshotMarker(v)
	if err != nil {
		if errors.Cause(err) != etcdserver.ErrServerStopped {
			return err
		}
		log.Debug("cannot place snapshot marker", zap.Error(err))
		return nil
	}
	log.Debug("placed snapshot marker",
		zap.String("key", string(etcdserver.SnapshotMarkerKey)),
		zap.String("value", string(v)),
		zap.Int64("rev", rev),
	)
	return nil
}

func (m *Manager) restoreFromSnapshot(peers []*etcdserver.Peer) (bool, error) {
	if m.snapshotter == nil {
		return false, nil
	}

	r, err := m.snapshotter.Load()
	if err != nil {
		return false, err
	}
	defer r.Close()

	log.Debugf("[%v]: attempting snapshot restore with members: %s", shortName(m.etcd.Name), peers)
	tmpFile, err := ioutil.TempFile("", "snapshot.load")
	if err != nil {
		return false, err
	}
	defer tmpFile.Close()

	r = snapshotutil.NewGunzipReadCloser(r)
	r = snapshotutil.NewDecrypterReadCloser(r, m.snapshotEncryptionKey)
	if _, err := io.Copy(tmpFile, r); err != nil {
		return false, err
	}

	// if the process is restarted, this will fail if the data-dir already
	// exists, so it must be deleted here
	if err := os.RemoveAll(m.cfg.DataDir); err != nil {
		log.Errorf("cannot remove data-dir: %v", err)
	}
	log.Infof("loading snapshot from: %#v", tmpFile.Name())
	if err := m.etcd.RestoreSnapshot(tmpFile.Name(), peers); err != nil {
		return false, err
	}
	log.Infof("successfully loaded snapshot from: %#v", tmpFile.Name())
	return true, nil
}

// joinEtcdCluster attempts to join an etcd cluster by establishing a client
// connection with the provided peer URL.
func (m *Manager) joinEtcdCluster(peerURL string) error {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()

	cc, err := client.New(&client.Config{
		ClientURLs:     []string{peerURL},
		SecurityConfig: m.etcd.PeerSecurity,
		Timeout:        1 * time.Second,
	})
	c := &Client{
		Client:  cc,
		Timeout: 1 * time.Second,
	}
	if err != nil {
		return err
	}
	defer c.Close()

	members, err := c.members(ctx)
	if err != nil {
		return err
	}

	// In cases where the existing cluster identifies this instance to already
	// be a member of the cluster, we attempt to start right away. This case
	// happens when restarting a node and specifying the previous node name.
	// The previous node name MUST be specified since otherwise a new Name is
	// generated.
	if members[m.etcd.Name] != nil {
		peers := make([]*etcdserver.Peer, 0)
		for _, m := range members {
			peers = append(peers, &etcdserver.Peer{Name: m.Name, URL: m.PeerURL})
		}
		log.Infof("%s is already considered a member, attempting to start ...", m.etcd.Name)
		if err := m.etcd.JoinExisting(ctx, peers); err == nil {
			return nil
		}
		log.Infof("%s is already considered a member, but failed to start, attempting to remove ...", m.etcd.Name)
		if err := c.removeMemberLocked(ctx, members[m.etcd.Name]); err != nil {
			return err
		}
	}

	log.Infof("%s is NOT a member, attempting to add member and start ...", m.etcd.Name)
	if err := os.RemoveAll(m.cfg.DataDir); err != nil {
		log.Errorf("failed to remove data dir %s, %v", m.cfg.DataDir, err)
	}
	unlock, err := c.Lock(m.etcd.Name, 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()

	member, err := c.addMember(ctx, m.etcd.PeerURL.String())
	if err != nil {
		return err
	}

	// The name will not be available immediately after adding a new member.
	// Since the member missing is this member, we can safely use the local
	// member name.
	peers := []*etcdserver.Peer{{Name: m.etcd.Name, URL: m.etcd.PeerURL.String()}}
	for _, m := range members {
		peers = append(peers, &etcdserver.Peer{Name: m.Name, URL: m.PeerURL})
	}
	if err := m.etcd.JoinExisting(ctx, peers); err != nil {
		if err := c.removeMember(m.ctx, member.ID); err != nil {
			log.Debug("unable to remove member", zap.Error(err))
		}
		return err
	}
	return nil
}
