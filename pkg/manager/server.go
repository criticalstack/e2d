package manager

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/netutil"
	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/clientv3/snapshot"
	"go.etcd.io/etcd/embed"
	"go.etcd.io/etcd/etcdserver/api/membership"
	"go.etcd.io/etcd/mvcc/backend"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/grpclog"
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

	Debug bool
}

type server struct {
	*embed.Etcd
	cfg *serverConfig

	// used to determine if the instance of Etcd has already been started
	started uint64
}

func newServer(cfg *serverConfig) *server {
	return &server{cfg: cfg}
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

func (s *server) hardStop() {
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

func (s *server) startEtcd(state string, peers []*Peer) error {
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

	// XXX(chris): not sure about this
	clientv3.SetLogger(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))

	// TODO(chris): Etcd requires quorum when this in enabled, meaning that two
	// nodes only could not be reconfigured since they will never be greater
	// than N+1, so we disable this. It is possible that this can be enabled,
	// however, we have to ensure that etcd can lose one member of a 3 node
	// cluster and still be able to recover specifically by removing the
	// previous member and adding a new one.
	cfg.StrictReconfigCheck = false

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
		log.Info("Server is ready!")
		atomic.StoreUint64(&s.started, 1)

		go func() {
			<-s.Server.StopNotify()
			atomic.StoreUint64(&s.started, 0)
		}()
	case <-time.After(300 * time.Second):
		s.Server.Stop()
		log.Info("Server took too long to start!")
	case err := <-s.Err():
		return errors.Wrap(err, "etcd.Server.Start")
	}
	return nil
}

func (s *server) startNew(peers []*Peer) error {
	return s.startEtcd(embed.ClusterStateFlagNew, peers)
}

func (s *server) joinExisting(peers []*Peer) error {
	return s.startEtcd(embed.ClusterStateFlagExisting, peers)
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
