package manager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/clientv3/snapshot"
	"go.etcd.io/etcd/embed"
	"go.etcd.io/etcd/etcdserver/api/membership"
	"go.etcd.io/etcd/lease"
	"go.etcd.io/etcd/mvcc"
	"go.etcd.io/etcd/mvcc/backend"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/e2db"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/netutil"
)

type serverConfig struct {
	// name used for etcd.Embed instance, should generally be left alone so
	// that a random name is generated
	Name string

	// directory used for etcd data-dir, wal and snapshot dirs derived from
	// this by etcd
	Dir string

	// client endpoint for accessing etcd
	ClientURL url.URL

	// address used for traffic within the cluster
	PeerURL url.URL

	// the required number of nodes that must be present to start a cluster
	RequiredClusterSize int

	// configures authentication/transport security for clients
	ClientSecurity client.SecurityConfig

	// configures authentication/transport security within the etcd cluster
	PeerSecurity client.SecurityConfig

	// add a local client listener (i.e. 127.0.0.1)
	EnableLocalListener bool

	// configures the level of the logger used by etcd
	EtcdLogLevel zapcore.Level

	ServiceRegister func(*grpc.Server)

	Debug bool
}

type server struct {
	*embed.Etcd
	cfg *serverConfig

	// used to determine if the instance of Etcd has already been started
	started uint64
	// set when server is being restarted
	restarting uint64

	// mu is used to coordinate potentially unsafe access to etcd
	mu sync.Mutex
}

func newServer(cfg *serverConfig) *server {
	return &server{cfg: cfg}
}

func (s *server) isRestarting() bool {
	return atomic.LoadUint64(&s.restarting) == 1
}

func (s *server) isRunning() bool {
	return atomic.LoadUint64(&s.started) == 1
}

func (s *server) isLeader() bool {
	if !s.isRunning() {
		return false
	}
	return s.Etcd.Server.Leader() == s.Etcd.Server.ID()
}

func (s *server) restart(ctx context.Context, peers []*Peer) error {
	atomic.StoreUint64(&s.restarting, 1)
	defer atomic.StoreUint64(&s.restarting, 0)

	s.hardStop()
	return s.startEtcd(ctx, embed.ClusterStateFlagNew, peers)
}

func (s *server) hardStop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Etcd != nil {
		// This shuts down the etcdserver.Server instance without coordination
		// with other members of the cluster. This ensures that a transfer of
		// leadership is not attempted, which can cause an issue where a member
		// can no longer join after a snapshot restore, should it fail during
		// the attempted transfer of leadership.
		s.Server.HardStop()

		// This must be called after HardStop since Close will start a graceful
		// shutdown.
		s.Etcd.Close()
	}
	atomic.StoreUint64(&s.started, 0)
}

func (s *server) gracefulStop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Etcd != nil {
		// There is no need to call Stop on the underlying etcdserver.Server
		// since it is called in Close.
		s.Etcd.Close()
	}
	atomic.StoreUint64(&s.started, 0)
}

type Peer struct {
	Name string
	URL  string
}

func (p *Peer) String() string {
	return fmt.Sprintf("%s=%s", p.Name, p.URL)
}

func initialClusterStringFromPeers(peers []*Peer) string {
	initialCluster := make([]string, 0)
	for _, p := range peers {
		initialCluster = append(initialCluster, fmt.Sprintf("%s=%s", p.Name, p.URL))
	}
	if len(initialCluster) == 0 {
		return ""
	}
	sort.Strings(initialCluster)
	return strings.Join(initialCluster, ",")
}

// validatePeers ensures that a group of peers are capable of starting, joining
// or recovering a cluster. It must be used whenever the initial cluster string
// will be built.
func validatePeers(peers []*Peer, requiredClusterSize int) error {
	// When the name part of the initial cluster string is blank, etcd behaves
	// abnormally. The same raft id is generated when providing the same
	// connection information, so in cases where a member was removed from the
	// cluster and replaced by a new member with the same address, having a
	// blank name caused it to not be removed from the removed member tracking
	// done by rafthttp. The member is "accepted" into the cluster, but cannot
	// participate since the transport layer won't allow it.
	for _, p := range peers {
		if p.Name == "" || p.URL == "" {
			return errors.Errorf("peer name/url cannot be blank: %+v", p)
		}
	}

	// The number of peers used to start etcd should always be the same as the
	// cluster size. Otherwise, the etcd cluster can (very likely) fail to
	// become healthy, therefore we go ahead and return early rather than deal
	// with an invalid state.
	if len(peers) < requiredClusterSize {
		return errors.Errorf("expected %d members, but received %d: %v", requiredClusterSize, len(peers), peers)
	}
	return nil
}

func (s *server) startEtcd(ctx context.Context, state string, peers []*Peer) error {
	if err := validatePeers(peers, s.cfg.RequiredClusterSize); err != nil {
		return err
	}

	cfg := embed.NewConfig()
	cfg.Name = s.cfg.Name
	cfg.Dir = s.cfg.Dir
	if s.cfg.Dir != "" {
		cfg.Dir = s.cfg.Dir
	}
	if err := os.MkdirAll(cfg.Dir, 0755); err != nil && !os.IsExist(err) {
		return errors.Wrapf(err, "cannot create etcd data dir: %#v", cfg.Dir)
	}
	cfg.Logger = "zap"
	cfg.Debug = s.cfg.Debug
	cfg.ZapLoggerBuilder = func(c *embed.Config) error {
		l := log.NewLoggerWithLevel("etcd", s.cfg.EtcdLogLevel)
		return embed.NewZapCoreLoggerBuilder(l, l.Core(), zapcore.AddSync(os.Stderr))(c)
	}
	cfg.AutoCompactionMode = embed.CompactorModePeriodic
	cfg.LPUrls = []url.URL{s.cfg.PeerURL}
	cfg.APUrls = []url.URL{s.cfg.PeerURL}
	cfg.LCUrls = []url.URL{s.cfg.ClientURL}
	if s.cfg.EnableLocalListener {
		_, port, _ := netutil.SplitHostPort(s.cfg.ClientURL.Host)
		cfg.LCUrls = append(cfg.LCUrls, url.URL{Scheme: s.cfg.ClientSecurity.Scheme(), Host: fmt.Sprintf("127.0.0.1:%d", port)})
	}
	cfg.ACUrls = []url.URL{s.cfg.ClientURL}
	cfg.ClientAutoTLS = s.cfg.ClientSecurity.AutoTLS
	cfg.PeerAutoTLS = s.cfg.PeerSecurity.AutoTLS
	if s.cfg.ClientSecurity.Enabled() {
		cfg.ClientTLSInfo = s.cfg.ClientSecurity.TLSInfo()
	}
	if s.cfg.PeerSecurity.Enabled() {
		cfg.PeerTLSInfo = s.cfg.PeerSecurity.TLSInfo()
	}
	cfg.EnableV2 = false
	cfg.ClusterState = state
	cfg.InitialCluster = initialClusterStringFromPeers(peers)
	cfg.StrictReconfigCheck = true
	cfg.ServiceRegister = s.cfg.ServiceRegister

	// XXX(chris): not sure about this
	clientv3.SetLogger(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))

	log.Info("starting etcd",
		zap.String("name", cfg.Name),
		zap.String("dir", s.cfg.Dir),
		zap.String("cluster-state", cfg.ClusterState),
		zap.String("initial-cluster", cfg.InitialCluster),
		zap.Int("required-cluster-size", s.cfg.RequiredClusterSize),
		zap.Bool("debug", s.cfg.Debug),
	)
	var err error
	s.Etcd, err = embed.StartEtcd(cfg)
	if err != nil {
		return err
	}
	select {
	case <-s.Server.ReadyNotify():
		if err := s.writeClusterInfo(ctx); err != nil {
			return errors.Wrap(err, "cannot write cluster-info")
		}
		log.Debug("write cluster-info successful!")
		atomic.StoreUint64(&s.started, 1)
		log.Info("Server is ready!")

		go func() {
			<-s.Server.StopNotify()
			atomic.StoreUint64(&s.started, 0)
		}()
		return nil
	case err := <-s.Err():
		return errors.Wrap(err, "etcd.Server.Start")
	case <-ctx.Done():
		s.Server.Stop()
		log.Info("Server was unable to start")
		return ctx.Err()
	}
}

func (s *server) startNew(ctx context.Context, peers []*Peer) error {
	return s.startEtcd(ctx, embed.ClusterStateFlagNew, peers)
}

func (s *server) joinExisting(ctx context.Context, peers []*Peer) error {
	return s.startEtcd(ctx, embed.ClusterStateFlagExisting, peers)
}

func newSnapshotReadCloser(snapshot backend.Snapshot) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		n, err := snapshot.WriteTo(pw)
		if err == nil {
			log.Infof("wrote database snapshot out [total bytes: %d]", n)
		}
		_ = pw.CloseWithError(err)
		snapshot.Close()
	}()
	return pr
}

func (s *server) createSnapshot(minRevision int64) (io.ReadCloser, int64, int64, error) {
	// Get the current revision and compare with the minimum requested revision.
	revision := s.Etcd.Server.KV().Rev()
	if revision <= minRevision {
		return nil, 0, revision, errors.Errorf("member revision too old, wanted %d, received: %d", minRevision, revision)
	}
	sp := s.Etcd.Server.Backend().Snapshot()
	if sp == nil {
		return nil, 0, revision, errors.New("no snappy")
	}
	return newSnapshotReadCloser(sp), sp.Size(), revision, nil
}

func (s *server) restoreSnapshot(snapshotFilename string, peers []*Peer) error {
	if err := validatePeers(peers, s.cfg.RequiredClusterSize); err != nil {
		return err
	}
	snapshotMgr := snapshot.NewV3(nil)
	return snapshotMgr.Restore(snapshot.RestoreConfig{
		// SnapshotPath is the path of snapshot file to restore from.
		SnapshotPath: snapshotFilename,

		// Name is the human-readable name of this member.
		Name: s.cfg.Name,

		// OutputDataDir is the target data directory to save restored data.
		// OutputDataDir should not conflict with existing etcd data directory.
		// If OutputDataDir already exists, it will return an error to prevent
		// unintended data directory overwrites.
		// If empty, defaults to "[Name].etcd" if not given.
		OutputDataDir: s.cfg.Dir,

		// PeerURLs is a list of member's peer URLs to advertise to the rest of the cluster.
		PeerURLs: []string{s.cfg.PeerURL.String()},

		// InitialCluster is the initial cluster configuration for restore bootstrap.
		InitialCluster: initialClusterStringFromPeers(peers),

		InitialClusterToken: embed.NewConfig().InitialClusterToken,
		SkipHashCheck:       true,
	})
}

var (
	errCannotFindMember = errors.New("cannot find member")
)

func (s *server) lookupMember(name string) (uint64, error) {
	for _, member := range s.Etcd.Server.Cluster().Members() {
		if member.Name == name {
			return uint64(member.ID), nil
		}
	}
	return 0, errors.Wrap(errCannotFindMember, name)
}

func (s *server) lookupMemberNameByPeerAddr(addr string) (string, error) {
	for _, member := range s.Etcd.Server.Cluster().Members() {
		for _, url := range member.PeerURLs {
			if url == addr {
				return member.Name, nil
			}
		}
	}
	return "", errors.Wrap(errCannotFindMember, addr)
}

func (s *server) removeMember(ctx context.Context, name string) error {
	id, err := s.lookupMember(name)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err = s.Server.RemoveMember(cctx, id)
	if err != nil && err != membership.ErrIDRemoved {
		return errors.Errorf("cannot remove member %#v: %v", name, err)
	}
	return nil
}

type Cluster struct {
	ID                  int `e2db:"id"`
	Created             time.Time
	RequiredClusterSize int
}

// writeClusterInfo attempts to write basic cluster info whenever a server
// starts or joins a new cluster. The e2db namespace matches the volatile
// prefix so that this information will not survive being restored from
// snapshot. This is because the cluster requirements could change for the
// restored cluster (e.g. going from RequiredClusterSize 1 -> 3).
func (s *server) writeClusterInfo(ctx context.Context) error {
	// NOTE(chrism): As the naming can be confusing it is worth pointing out
	// that the ClientSecurity field is specifying the server certs and NOT the
	// client certs. Since the server certs do not have client auth key usage,
	// we need to use the peer certs here (they have client auth key usage).
	db, err := e2db.New(ctx, &e2db.Config{
		ClientAddr: s.cfg.ClientURL.String(),
		CAFile:     s.cfg.PeerSecurity.TrustedCAFile,
		CertFile:   s.cfg.PeerSecurity.CertFile,
		KeyFile:    s.cfg.PeerSecurity.KeyFile,
		Namespace:  string(volatilePrefix),
	})
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Table(new(Cluster)).Tx(func(tx *e2db.Tx) error {
		var cluster *Cluster
		if err := tx.Find("ID", 1, &cluster); err != nil && errors.Cause(err) != e2db.ErrNoRows {
			return err
		}

		if cluster != nil {
			// check RequiredClusterSize for discrepancies
			if cluster.RequiredClusterSize != s.cfg.RequiredClusterSize {
				return errors.Errorf("server %s attempted to join cluster with incorrect RequiredClusterSize, cluster expects %d, this server is configured with %d", s.cfg.Name, cluster.RequiredClusterSize, s.cfg.RequiredClusterSize)
			}
			return nil
		}

		return tx.Insert(&Cluster{
			ID:                  1,
			Created:             time.Now(),
			RequiredClusterSize: s.cfg.RequiredClusterSize,
		})
	})
}

var (
	// volatilePrefix is the key prefix used for keys that will NOT be
	// preserved after a cluster is recovered from snapshot
	volatilePrefix = []byte("/_e2d")

	// snapshotMarkerKey is the key used to indicate when a cluster recovered
	// from snapshot
	snapshotMarkerKey = []byte("/_e2d/snapshot")
)

var errServerStopped = errors.New("server stopped")

func (s *server) clearVolatilePrefix() (rev, deleted int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning() {
		return 0, 0, errServerStopped
	}
	res, err := s.Server.KV().Range(volatilePrefix, []byte{}, mvcc.RangeOptions{})
	if err != nil {
		return 0, 0, err
	}
	for _, kv := range res.KVs {
		if bytes.HasPrefix(kv.Key, volatilePrefix) {
			n, _ := s.Server.KV().DeleteRange(kv.Key, nil)
			deleted += n
		}
	}
	return res.Rev, deleted, nil
}

func (s *server) placeSnapshotMarker(v []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning() {
		return 0, errServerStopped
	}
	rev := s.Server.KV().Put(snapshotMarkerKey, v, lease.NoLease)
	return rev, nil
}
