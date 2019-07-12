package manager

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/gziputil"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/snapshot"
	"github.com/hashicorp/memberlist"
	"github.com/pkg/errors"
	"go.etcd.io/etcd/lease"
	"go.etcd.io/etcd/mvcc"
	"go.uber.org/zap"
)

// Manager manages an embedded etcd instance.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg         *Config
	gossip      *gossip
	etcd        *server
	cluster     *clusterMembership
	snapshotter snapshot.SnapshotProvider
	self        *Member

	removeCh chan string
}

// New creates a new instance of Manager.
func New(cfg *Config) (*Manager, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	m := &Manager{
		cfg: cfg,
		etcd: newServer(&serverConfig{
			Name:                cfg.Name,
			Dir:                 cfg.Dir,
			ClientURL:           cfg.ClientURL,
			PeerURL:             cfg.PeerURL,
			RequiredClusterSize: cfg.RequiredClusterSize,
			ClientSecurity:      cfg.ClientSecurity,
			PeerSecurity:        cfg.PeerSecurity,
			Debug:               cfg.Debug,
			EnableLocalListener: true,
		}),
		gossip: newGossip(&gossipConfig{
			Name:       cfg.Name,
			ClientURL:  cfg.ClientURL.String(),
			PeerURL:    cfg.PeerURL.String(),
			GossipHost: cfg.GossipHost,
			GossipPort: cfg.GossipPort,
			SecretKey:  cfg.GossipSecretKey,
		}),
		removeCh:    make(chan string, 10),
		snapshotter: cfg.SnapshotProvider,
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.cluster = newClusterMembership(m.ctx, m.cfg.HealthCheckTimeout, func(name string) error {
		log.Debug("removing member ...",
			zap.String("name", shortName(m.cfg.Name)),
			zap.String("removed", shortName(name)),
		)
		if err := m.etcd.removeMember(m.ctx, name); err != nil && errors.Cause(err) != errCannotFindMember {
			return err
		}
		log.Debug("member removed",
			zap.String("name", shortName(m.cfg.Name)),
			zap.String("removed", shortName(name)),
		)

		// TODO(chris): this is mostly used for testing atm and
		// should evolve in the future to be part of a more
		// complete event broadcast system
		select {
		case m.removeCh <- name:
		default:
		}
		return nil
	})
	return m, nil
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
	m.etcd.hardStop()
	m.gossip.Shutdown()
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
	m.etcd.gracefulStop()
	m.gossip.Shutdown()
}

func (m *Manager) restoreFromSnapshot(peers []*Peer) (bool, error) {
	if m.snapshotter == nil {
		return false, nil
	}

	r, err := m.snapshotter.Load()
	if err != nil {
		return false, err
	}
	defer r.Close()
	log.Debugf("[%v]: attempting snapshot restore with members: %s", shortName(m.cfg.Name), peers)
	tmpFile, err := ioutil.TempFile("", "snapshot.load")
	if err != nil {
		return false, err
	}
	defer tmpFile.Close()
	if m.cfg.SnapshotCompression {
		var err error
		r, err = gziputil.NewGunzipReadCloser(r)
		if err != nil {
			return false, err
		}
	}
	if _, err := io.Copy(tmpFile, r); err != nil {
		return false, err
	}

	// if the process is restarted, this will fail if the data-dir already
	// exists, so it must be deleted here
	if err := os.RemoveAll(m.cfg.Dir); err != nil {
		log.Errorf("cannot remove data-dir: %v", err)
	}
	log.Infof("loading snapshot from: %#v", tmpFile.Name())
	if err := m.etcd.restoreSnapshot(tmpFile.Name(), peers); err != nil {
		return false, err
	}
	log.Infof("successfully loaded snapshot from: %#v", tmpFile.Name())
	return true, nil
}

var (
	// volatilePrefix is the key prefix used for keys that will NOT be
	// preserved after a cluster is recovered from snapshot
	volatilePrefix = []byte("/_e2d")

	// snapshotMarkerKey is the key used to indicate when a cluster recovered
	// from snapshot
	snapshotMarkerKey = []byte("/_e2d/snapshot")
)

// startEtcdCluster starts a new etcd cluster with the provided peers. The list
// of peers provided must be inclusive of this prospective instance. An attempt
// is made to restore from a previous snapshot when one is available.
//
// When restoring from a snapshot, all volatile keys are deleted and a snapshot
// marker is created. This enables clients using e2d to coordinate their
// cluster, by conveying information about whether this is a brand new cluster
// or an existing cluster that recovered from total cluster failure.
func (m *Manager) startEtcdCluster(peers []*Peer) error {
	snapshot, err := m.restoreFromSnapshot(peers)
	if err != nil {
		log.Error("cannot restore snapshot", zap.Error(err))
	}
	if err := m.etcd.startNew(peers); err != nil {
		return err
	}
	if !snapshot {
		return nil
	}

	// These operations directly interact with the etcd key/value store,
	// therefore do NOT get committed through the raft log. This is OK
	// since all servers that recover from a snapshot will perform the same
	// operations and the outcome is deterministic.
	res, err := m.etcd.Server.KV().Range(volatilePrefix, []byte{}, mvcc.RangeOptions{})
	if err != nil {
		return err
	}
	var deleted int64
	for _, kv := range res.KVs {
		if bytes.HasPrefix(kv.Key, volatilePrefix) {
			n, _ := m.etcd.Server.KV().DeleteRange(kv.Key, nil)
			deleted += n
		}
	}
	log.Debug("deleted volatile keys",
		zap.Int64("deleted-keys", deleted),
		zap.Int64("revision", res.Rev),
	)
	v := []byte(time.Now().Format(time.RFC3339))
	rev := m.etcd.Server.KV().Put(snapshotMarkerKey, v, lease.NoLease)
	log.Debug("placed snapshot marker",
		zap.String("key", string(snapshotMarkerKey)),
		zap.String("value", string(v)),
		zap.Int64("rev", rev),
	)
	return nil
}

// joinEtcdCluster attempts to join an etcd cluster by establishing a client
// connection with the provided peer URL.
func (m *Manager) joinEtcdCluster(peerURL string) error {
	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()

	c, err := newClient(&client.Config{
		ClientURLs:     []string{peerURL},
		SecurityConfig: m.cfg.PeerSecurity,
		Timeout:        1 * time.Second,
	})
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
	if members[m.cfg.Name] != nil {
		peers := make([]*Peer, 0)
		for _, m := range members {
			peers = append(peers, &Peer{m.Name, m.PeerURL})
		}
		log.Infof("%s is already considered a member, attempting to start ...", m.cfg.Name)
		if err := m.etcd.joinExisting(peers); err == nil {
			return nil
		}
		log.Infof("%s is already considered a member, but failed to start, attempting to remove ...", m.cfg.Name)
		if err := c.removeMemberLocked(ctx, members[m.cfg.Name]); err != nil {
			return err
		}
	}

	log.Infof("%s is NOT a member, attempting to add member and start ...", m.cfg.Name)
	if err := os.RemoveAll(m.cfg.Dir); err != nil {
		log.Errorf("failed to remove data dir %s, %v", m.cfg.Dir, err)
	}
	unlock, err := c.Lock(m.cfg.Name, 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	member, err := c.addMember(ctx, m.cfg.PeerURL.String())
	if err != nil {
		return err
	}

	// The name will not be available immediately after adding a new member.
	// Since the member missing is this member, we can safely use the local
	// member name.
	peers := []*Peer{{m.cfg.Name, m.cfg.PeerURL.String()}}
	for _, m := range members {
		peers = append(peers, &Peer{m.Name, m.PeerURL})
	}
	if err := m.etcd.joinExisting(peers); err != nil {
		c.removeMember(m.ctx, member.ID)
		return err
	}
	return nil
}

func (m *Manager) startOrJoinEtcdCluster() error {
	ctx, cancel := context.WithTimeout(m.ctx, m.cfg.BootstrapTimeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// first use peers to attempt joining an existing cluster
			for _, member := range m.gossip.Members() {
				if member.Name == m.cfg.Name {
					continue
				}
				log.Debugf("[%v]: gossip peer: %+v", shortName(m.cfg.Name), member)
				if member.Status != Running {
					log.Debugf("[%v]: cannot join peer %#v in current status: %s", shortName(m.cfg.Name), shortName(member.Name), member.Status)
					continue
				}
				if err := m.joinEtcdCluster(member.ClientURL); err != nil {
					log.Debugf("[%v]: cannot join node %#v: %v", shortName(m.cfg.Name), member.ClientURL, err)
					continue
				}
				log.Debug("joined an existing etcd cluster successfully")
				return nil
			}
			log.Debugf("[%v]: cluster currently has %d members", shortName(m.cfg.Name), len(m.gossip.Members()))
			if len(m.gossip.Members()) < m.cfg.RequiredClusterSize {
				continue
			}
			if m.gossip.self.Status != Pending {
				if err := m.gossip.Update(Pending); err != nil {
					log.Debugf("[%v]: cannot update member metadata: %v", shortName(m.cfg.Name), err)
				}
			}

			// when enough members are reporting in as pending, it means that a
			// majority of members were unable to connect to an existing
			// cluster
			if len(m.gossip.pendingMembers()) < m.cfg.RequiredClusterSize {
				log.Debugf("[%v]: members pending: %d", shortName(m.cfg.Name), len(m.gossip.pendingMembers()))
				continue
			}
			peers := make([]*Peer, 0)
			for _, m := range m.gossip.Members() {
				peers = append(peers, &Peer{m.Name, m.PeerURL})
			}
			return m.startEtcdCluster(peers)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) runMembershipCleanup() {
	if m.cfg.RequiredClusterSize == 1 {
		return
	}
	for {
		select {
		case ev := <-m.gossip.Events():
			log.Debugf("[%v]: received membership event: %v", shortName(m.cfg.Name), ev)

			// When this members gossip network does not have enough members to
			// be considered a majority, it is no longer eligible to affect
			// cluster membership. This helps ensure that when a network
			// partition takes place that minority partition(s) will not
			// attempt to change cluster membership. Only members in Running
			// status are considered.
			if !m.cluster.ensureQuorum(len(m.gossip.runningMembers()) > m.cfg.RequiredClusterSize/2) {
				log.Info("not enough members are healthy to remove other members",
					zap.String("name", shortName(m.cfg.Name)),
					zap.Int("gossip-members", len(m.gossip.runningMembers())),
					zap.Int("required-cluster-size", m.cfg.RequiredClusterSize),
				)
			}

			member := &Member{}
			if err := member.Unmarshal(ev.Node.Meta); err != nil {
				log.Debugf("[%v]: cannot unmarshal node meta: %v", shortName(m.cfg.Name), err)
				continue
			}

			// This member must not acknowledge membership changes related to
			// itself. Gossip events are used to determine when a member needs
			// to be evicted, and this responsibility falls to peers only (i.e.
			// a member should never evict itself). The PeerURL is used rather
			// than the name or gossip address as it better represents a
			// distinct member of the cluster as only one PeerURL will ever be
			// present on a network.
			if member.PeerURL == m.cfg.PeerURL.String() {
				continue
			}
			switch ev.Event {
			case memberlist.NodeJoin:
				log.Debugf("[%v]: member joined: %#v", shortName(m.cfg.Name), member.Name)

				// The name of the new member is compared with any members with
				// a matching PeerURL that are currently part of the etcd
				// cluster membership. In the case that a member is still part
				// of the etcd cluster membership, but has a different name
				// than the joining member, the assertion can be made that the
				// existing member is now defunct and can be removed
				// immediately to allow the new member to join. Since members
				// do not handle gossip events for their own PeerURL, this
				// check will only ever be performed by peers of the member
				// joining the gossip network.
				if oldName, err := m.etcd.lookupMemberNameByPeerAddr(member.PeerURL); err == nil {
					log.Debugf("[%v]: member %v peerAddr in use by member %v", shortName(m.cfg.Name), member.Name, oldName)
					if oldName != member.Name {
						log.Debugf("[%v]: members name mismatched, evicting %v", shortName(m.cfg.Name), oldName)
						m.cluster.removeMember(oldName)
					}
				}

				m.cluster.removeSuspect(member.Name)
			case memberlist.NodeLeave:
				m.cluster.addSuspect(member.Name)
			case memberlist.NodeUpdate:
			}
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) runSnapshotter() {
	if m.snapshotter == nil {
		log.Info("snapshotting disabled: no snapshot backup set")
		return
	}
	log.Debug("starting snapshotter")
	ticker := time.NewTicker(m.cfg.SnapshotInterval)
	defer ticker.Stop()

	var latestRev int64

	for {
		select {
		case <-ticker.C:
			if !m.etcd.isLeader() {
				log.Debug("not leader, skipping snapshot backup")
				continue
			}
			log.Debug("starting snapshot backup")
			snapshotData, rev, err := m.etcd.createSnapshot(latestRev)
			if err != nil {
				log.Debug("cannot create snapshot",
					zap.String("name", shortName(m.cfg.Name)),
					zap.Error(err),
				)
				continue
			}
			if m.cfg.SnapshotCompression {
				snapshotData = gziputil.NewGzipReadCloser(snapshotData, gzip.BestCompression)
			}
			if err := m.snapshotter.Save(snapshotData); err != nil {
				log.Debug("cannot save snapshot",
					zap.String("name", shortName(m.cfg.Name)),
					zap.Error(err),
				)
				continue
			}
			latestRev = rev
			log.Infof("wrote snapshot (rev %d) to backup", latestRev)
		case <-m.ctx.Done():
			log.Debug("stopping snapshotter")
			return
		}
	}
}

// Run starts and manages an etcd node based upon the provided configuration.
// In the case of a fault, or if the manager is otherwise stopped, this method
// exits.
func (m *Manager) Run() error {
	if m.etcd.isRunning() {
		return errors.New("etcd is already running")
	}

	switch m.cfg.RequiredClusterSize {
	case 1:
		// a single-node etcd cluster does not require gossip or need to wait for
		// other members and therefore can start immediately
		if err := m.startEtcdCluster([]*Peer{{m.cfg.Name, m.cfg.PeerURL.String()}}); err != nil {
			return err
		}
	case 3, 5:
		// all multi-node clusters require the gossip network to be started
		if err := m.gossip.Start(m.ctx, m.cfg.BootstrapAddrs); err != nil {
			return err
		}

		// a multi-node etcd cluster will either be created or an existing one will
		// be joined
		if err := m.startOrJoinEtcdCluster(); err != nil {
			return err
		}

		if err := m.gossip.Update(Running); err != nil {
			log.Debugf("[%v]: cannot update member metadata: %v", m.cfg.Name, err)
		}
	}

	// cluster is ready so start maintenance loops
	go m.runMembershipCleanup()
	go m.runSnapshotter()

	select {
	case <-m.etcd.Server.StopNotify():
		log.Info("etcd server stopping ...",
			zap.Stringer("id", m.etcd.Server.ID()),
			zap.String("name", m.cfg.Name),
		)
		if m.cfg.RequiredClusterSize == 1 {
			return nil
		}
		if err := m.gossip.Update(Unknown); err != nil {
			log.Debugf("[%v]: cannot update member metadata: %v", m.cfg.Name, err)
		}
		return nil
	case err := <-m.etcd.Err():
		return err
	case <-m.ctx.Done():
		return nil
	}
}
