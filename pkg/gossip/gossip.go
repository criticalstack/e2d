package gossip

import (
	"context"
	"fmt"
	stdlog "log"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/log"
	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

type Config struct {
	Name       string
	ClientURL  string
	PeerURL    string
	GossipHost string
	GossipPort int
	SecretKey  []byte
	LogLevel   zapcore.Level
}

type Gossip struct {
	m memberlister

	config *memberlist.Config
	events chan memberlist.NodeEvent

	broadcasts *memberlist.TransmitLimitedQueue
	mu         sync.RWMutex
	nodes      map[string]NodeStatus
	self       *Member
}

func New(cfg *Config) *Gossip {
	c := memberlist.DefaultLANConfig()
	c.Name = cfg.Name
	c.BindAddr = cfg.GossipHost
	c.BindPort = cfg.GossipPort
	c.Logger = stdlog.New(&logger{log.NewLoggerWithLevel("memberlist", cfg.LogLevel, zap.AddCallerSkip(2))}, "", 0)
	c.SecretKey = cfg.SecretKey

	g := &Gossip{
		m:      &noopMemberlist{},
		config: c,
		events: make(chan memberlist.NodeEvent, 100),
		nodes:  make(map[string]NodeStatus),
		self: &Member{
			Name:       cfg.Name,
			ClientURL:  cfg.ClientURL,
			PeerURL:    cfg.PeerURL,
			GossipAddr: fmt.Sprintf("%s:%d", cfg.GossipHost, cfg.GossipPort),
		},
	}
	g.broadcasts = &memberlist.TransmitLimitedQueue{
		NumNodes: func() int {
			return g.m.NumMembers()
		},
		RetransmitMult: 4,
	}
	c.Delegate = g
	c.Events = &memberlist.ChannelEventDelegate{Ch: g.events}
	return g
}

func (g *Gossip) Shutdown() error {
	if err := g.m.Shutdown(); err != nil {
		return err
	}
	if g.config.Events != nil {
		g.config.Events = nil
	}
	if g.events != nil {
		close(g.events)
		g.events = nil
	}
	return nil
}

// Start attempts to join a gossip network using the given bootstrap addresses.
func (g *Gossip) Start(ctx context.Context, baddrs []string) error {
	m, err := memberlist.Create(g.config)
	if err != nil {
		return err
	}
	g.m = m

	if err := g.Update(Unknown); err != nil {
		return err
	}

	peers := make([]string, 0)
	for _, addr := range baddrs {
		host, port, err := netutil.SplitHostPort(addr)
		if err != nil {
			return errors.Wrapf(err, "cannot split bootstrap address: %#v", addr)
		}
		if host == "" {
			host = "127.0.0.1"
		}
		if port == 0 {
			port = configv1alpha1.DefaultGossipPort
		}
		peers = append(peers, fmt.Sprintf("%s:%d", host, port))
	}

	log.Debug("attempting to join gossip network ...",
		zap.String("bootstrap-addrs", strings.Join(peers, ",")),
	)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			_, err := g.m.Join(peers)
			if err != nil {
				log.Errorf("cannot join gossip network: %v", err)
				continue
			}
			log.Debug("joined gossip network successfully")
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Events returns a read-only channel of memberlist events.
func (g *Gossip) Events() <-chan memberlist.NodeEvent { return g.events }

// Members returns all members currently participating in the gossip network.
func (g *Gossip) Members() []*Member {
	g.mu.RLock()
	defer g.mu.RUnlock()

	members := make([]*Member, 0)
	for _, m := range g.m.Members() {
		// A member may be in the memberlist but Meta may be nil if the local
		// metadata has yet to be propagated to this node. In this case, we
		// ignore that member considering it to not ready.
		if m.Meta == nil {
			continue
		}
		meta := &Member{}
		if err := meta.Unmarshal(m.Meta); err != nil {
			log.Debugf("cannot unmarshal member: %v", err)
			continue
		}

		// status information shared via delegate is presumed to be more
		// accurate
		if status, ok := g.nodes[meta.Name]; ok {
			meta.Status = status
		}
		members = append(members, meta)
	}
	return members
}

func (g *Gossip) PendingMembers() []*Member {
	members := make([]*Member, 0)
	for _, member := range g.Members() {
		if member.Status == Pending {
			members = append(members, member)
		}
	}
	return members
}

func (g *Gossip) RunningMembers() []*Member {
	members := make([]*Member, 0)
	for _, member := range g.Members() {
		if member.Status == Running {
			members = append(members, member)
		}
	}
	return members
}
