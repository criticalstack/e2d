package manager

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	stdlog "log"
	"strings"
	"sync"
	"time"

	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/netutil"
	"github.com/hashicorp/memberlist"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	DefaultGossipPort = 7980
)

type NodeStatus int

const (
	Unknown NodeStatus = iota
	Pending
	Running
)

func (s NodeStatus) String() string {
	switch s {
	case Unknown:
		return "Unknown"
	case Pending:
		return "Pending"
	case Running:
		return "Running"
	}
	return ""
}

type Member struct {
	ID             uint64
	Name           string
	ClientAddr     string
	ClientURL      string
	PeerAddr       string
	PeerURL        string
	GossipAddr     string
	BootstrapAddrs []string
	Status         NodeStatus
}

func (m *Member) Marshal() ([]byte, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(*m); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (m *Member) Unmarshal(data []byte) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(m)
}

type memberlister interface {
	Join([]string) (int, error)
	LocalNode() *memberlist.Node
	Members() []*memberlist.Node
	NumMembers() int
	Shutdown() error
}

type noopMemberlist struct{}

func (noopMemberlist) Join([]string) (int, error) {
	return 0, nil
}

func (noopMemberlist) LocalNode() *memberlist.Node {
	return &memberlist.Node{}
}

func (noopMemberlist) Members() []*memberlist.Node {
	return nil
}

func (noopMemberlist) NumMembers() int {
	return 0
}

func (noopMemberlist) Shutdown() error {
	return nil
}

type logger struct {
	l *zap.Logger
}

func (l *logger) Write(p []byte) (n int, err error) {
	msg := string(p)
	parts := strings.SplitN(msg, " ", 2)
	lvl := "[DEBUG]"
	if len(parts) > 1 {
		lvl = parts[0]
		msg = strings.TrimPrefix(parts[1], "memberlist: ")
	}

	switch lvl {
	case "[DEBUG]":
		l.l.Debug(msg)
	case "[WARN]":
		l.l.Warn(msg)
	case "[INFO]":
		l.l.Info(msg)
	}
	return len(p), nil
}

type gossipConfig struct {
	Name       string
	ClientURL  string
	PeerURL    string
	GossipHost string
	GossipPort int
	SecretKey  []byte
	Debug      bool
}

type gossip struct {
	m memberlister

	config *memberlist.Config
	events chan memberlist.NodeEvent

	broadcasts *memberlist.TransmitLimitedQueue
	mu         sync.RWMutex
	nodes      map[string]NodeStatus
	self       *Member
}

func newGossip(cfg *gossipConfig) *gossip {
	c := memberlist.DefaultLANConfig()
	c.Name = cfg.Name
	c.BindAddr = cfg.GossipHost
	c.BindPort = cfg.GossipPort
	c.Logger = stdlog.New(&logger{log.NewLoggerWithLevel("memberlist", zapcore.InfoLevel)}, "", 0)
	c.SecretKey = cfg.SecretKey

	g := &gossip{
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
		RetransmitMult: 3,
	}
	c.Delegate = g
	c.Events = &memberlist.ChannelEventDelegate{Ch: g.events}
	return g
}

func (g *gossip) Shutdown() error {
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
func (g *gossip) Start(ctx context.Context, baddrs []string) error {
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
			port = DefaultGossipPort
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

// msg implements the memberlist.Broadcast interface and is required to send
// messages over the gossip network
type msg struct {
	data []byte
}

func (m *msg) Invalidates(other memberlist.Broadcast) bool { return false }
func (m *msg) Message() []byte                             { return m.data }
func (m *msg) Finished()                                   {}

type statusMsg struct {
	Name   string
	Status NodeStatus
}

// Update uses the provided NodeStatus to updates the node metadata and
// broadcast the updated NodeStatus to all currently known members.
func (g *gossip) Update(status NodeStatus) error {
	g.mu.Lock()
	g.nodes[g.self.Name] = status
	g.self.Status = status
	g.mu.Unlock()
	data, err := g.self.Marshal()
	if err != nil {
		return err
	}
	g.m.LocalNode().Meta = data
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(statusMsg{Name: g.self.Name, Status: status}); err != nil {
		return err
	}
	g.broadcasts.QueueBroadcast(&msg{b.Bytes()})
	return nil
}

// Events returns a read-only channel of memberlist events.
func (g *gossip) Events() <-chan memberlist.NodeEvent { return g.events }

// Members returns all members currently participating in the gossip network.
func (g *gossip) Members() []*Member {
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

func (g *gossip) pendingMembers() []*Member {
	members := make([]*Member, 0)
	for _, member := range g.Members() {
		if member.Status == Pending {
			members = append(members, member)
		}
	}
	return members
}

func (g *gossip) runningMembers() []*Member {
	members := make([]*Member, 0)
	for _, member := range g.Members() {
		if member.Status == Running {
			members = append(members, member)
		}
	}
	return members
}

func (g *gossip) NodeMeta(limit int) []byte { return g.m.LocalNode().Meta }

func (g *gossip) NotifyMsg(data []byte) {
	if len(data) == 0 {
		return
	}
	var n statusMsg
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&n); err != nil {
		log.Debugf("cannot unmarshal: %v", err)
		return
	}
	g.mu.Lock()
	g.nodes[n.Name] = n.Status
	g.mu.Unlock()
}

func (g *gossip) GetBroadcasts(overhead, limit int) [][]byte {
	return g.broadcasts.GetBroadcasts(overhead, limit)
}

func (g *gossip) LocalState(join bool) []byte {
	return nil
}

func (g *gossip) MergeRemoteState(buf []byte, join bool) {
}
