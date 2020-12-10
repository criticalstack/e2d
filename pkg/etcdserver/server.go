package etcdserver

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/embed"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/log"
	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

type Server struct {
	*embed.Etcd

	// name used for etcd.Embed instance, should generally be left alone so
	// that a random name is generated
	Name string

	// directory used for etcd data-dir, wal and snapshot dirs derived from
	// this by etcd
	DataDir string

	// client endpoint for accessing etcd
	ClientURL url.URL

	// address used for traffic within the cluster
	PeerURL url.URL

	// address serving prometheus metrics
	MetricsURL *url.URL

	// the required number of nodes that must be present to start a cluster
	RequiredClusterSize int

	// configures authentication/transport security for clients
	ClientSecurity client.SecurityConfig

	// configures authentication/transport security within the etcd cluster
	PeerSecurity client.SecurityConfig

	// add a local client listener (i.e. 127.0.0.1)
	EnableLocalListener bool

	CompactionInterval time.Duration

	PreVote bool

	UnsafeNoFsync bool

	// configures the level of the logger used by etcd
	LogLevel zapcore.Level

	ServiceRegister func(*grpc.Server)

	// used to determine if the instance of Etcd has already been started
	started uint64
	// set when server is being restarted
	restarting uint64

	// mu is used to coordinate potentially unsafe access to etcd
	mu sync.Mutex
}

func (s *Server) IsRestarting() bool {
	return atomic.LoadUint64(&s.restarting) == 1
}

func (s *Server) IsRunning() bool {
	return atomic.LoadUint64(&s.started) == 1
}

func (s *Server) IsLeader() bool {
	if !s.IsRunning() {
		return false
	}
	return s.Etcd.Server.Leader() == s.Etcd.Server.ID()
}

func (s *Server) Restart(ctx context.Context, peers []*Peer) error {
	atomic.StoreUint64(&s.restarting, 1)
	defer atomic.StoreUint64(&s.restarting, 0)

	s.HardStop()
	return s.startEtcd(ctx, embed.ClusterStateFlagNew, peers)
}

func (s *Server) HardStop() {
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

func (s *Server) GracefulStop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Etcd != nil {
		// There is no need to call Stop on the underlying etcdserver.Server
		// since it is called in Close.
		s.Etcd.Close()
	}
	atomic.StoreUint64(&s.started, 0)
}

func (s *Server) StartNew(ctx context.Context, peers []*Peer) error {
	return s.startEtcd(ctx, embed.ClusterStateFlagNew, peers)
}

func (s *Server) JoinExisting(ctx context.Context, peers []*Peer) error {
	return s.startEtcd(ctx, embed.ClusterStateFlagExisting, peers)
}

func (s *Server) startEtcd(ctx context.Context, state string, peers []*Peer) error {
	if err := validatePeers(peers, s.RequiredClusterSize); err != nil {
		return err
	}

	cfg := embed.NewConfig()
	cfg.Name = s.Name
	cfg.Dir = s.DataDir
	if s.DataDir != "" {
		cfg.Dir = s.DataDir
	}
	if err := os.MkdirAll(cfg.Dir, 0700); err != nil && !os.IsExist(err) {
		return errors.Wrapf(err, "cannot create etcd data dir: %#v", cfg.Dir)
	}

	// NOTE(chrism): etcd 3.4.9 introduced a check on the data directory
	// permissions that require 0700. Since this causes the server to not come
	// up we will attempt to change the perms.
	log.Info("chmod data dir", zap.String("dir", s.DataDir))
	if err := os.Chmod(cfg.Dir, 0700); err != nil {
		log.Error("chmod failed", zap.String("dir", s.DataDir), zap.Error(err))
	}
	cfg.Logger = "zap"
	if s.LogLevel == zapcore.DebugLevel {
		cfg.Debug = true
	}
	cfg.ZapLoggerBuilder = func(c *embed.Config) error {
		l := log.NewLoggerWithLevel("etcd", s.LogLevel)
		return embed.NewZapCoreLoggerBuilder(l, l.Core(), zapcore.AddSync(os.Stderr))(c)
	}
	cfg.PreVote = s.PreVote
	cfg.UnsafeNoFsync = s.UnsafeNoFsync
	cfg.AutoCompactionMode = embed.CompactorModePeriodic
	if s.CompactionInterval != 0 {
		cfg.AutoCompactionRetention = s.CompactionInterval.String()
	}
	cfg.LPUrls = []url.URL{s.PeerURL}
	cfg.APUrls = []url.URL{s.PeerURL}
	cfg.LCUrls = []url.URL{s.ClientURL}
	if s.EnableLocalListener {
		_, port, _ := netutil.SplitHostPort(s.ClientURL.Host)
		cfg.LCUrls = append(cfg.LCUrls, url.URL{Scheme: s.ClientSecurity.Scheme(), Host: fmt.Sprintf("127.0.0.1:%d", port)})
	}
	cfg.ACUrls = []url.URL{s.ClientURL}
	cfg.ClientAutoTLS = s.ClientSecurity.AutoTLS
	cfg.PeerAutoTLS = s.PeerSecurity.AutoTLS
	if s.ClientSecurity.Enabled() {
		cfg.ClientTLSInfo = s.ClientSecurity.TLSInfo()
	}
	if s.PeerSecurity.Enabled() {
		cfg.PeerTLSInfo = s.PeerSecurity.TLSInfo()
	}
	if s.MetricsURL != nil {
		cfg.ListenMetricsUrls = []url.URL{*s.MetricsURL}
		if s.EnableLocalListener {
			_, port, _ := netutil.SplitHostPort(s.MetricsURL.Host)
			cfg.ListenMetricsUrls = append(cfg.ListenMetricsUrls, url.URL{Scheme: s.MetricsURL.Scheme, Host: fmt.Sprintf("127.0.0.1:%d", port)})
		}
	}
	cfg.EnableV2 = false
	cfg.ClusterState = state
	cfg.InitialCluster = initialClusterStringFromPeers(peers)
	cfg.StrictReconfigCheck = true
	cfg.ServiceRegister = s.ServiceRegister

	// XXX(chris): not sure about this
	clientv3.SetLogger(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))

	log.Info("starting etcd",
		zap.String("name", cfg.Name),
		zap.String("dir", s.DataDir),
		zap.String("cluster-state", cfg.ClusterState),
		zap.String("initial-cluster", cfg.InitialCluster),
		zap.Int("required-cluster-size", s.RequiredClusterSize),
		zap.Bool("debug", cfg.Debug),
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
