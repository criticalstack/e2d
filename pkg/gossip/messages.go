package gossip

import (
	"bytes"
	"encoding/gob"

	"github.com/hashicorp/memberlist"
	"go.uber.org/zap"

	"github.com/criticalstack/e2d/pkg/log"
)

// Update uses the provided NodeStatus to updates the node metadata and
// broadcast the updated NodeStatus to all currently known members.
func (g *Gossip) Update(status NodeStatus) error {
	log.Debug("attempting to update node status",
		zap.String("name", g.self.Name),
		zap.Stringer("status", g.nodes[g.self.Name]),
		zap.Stringer("update-status", status),
	)
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

func (g *Gossip) NodeMeta(limit int) []byte { return g.m.LocalNode().Meta }

func (g *Gossip) NotifyMsg(data []byte) {
	if len(data) == 0 {
		return
	}
	var n statusMsg
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&n); err != nil {
		log.Debugf("cannot unmarshal: %v", err)
		return
	}
	log.Debug("received status update",
		zap.String("name", g.self.Name),
		zap.String("peer", n.Name),
		zap.Stringer("peer-status", n.Status),
	)
	g.mu.Lock()
	g.nodes[n.Name] = n.Status
	g.mu.Unlock()
}

func (g *Gossip) GetBroadcasts(overhead, limit int) [][]byte {
	return g.broadcasts.GetBroadcasts(overhead, limit)
}

func (g *Gossip) LocalState(join bool) []byte {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(statusMsg{Name: g.self.Name, Status: g.self.Status}); err != nil {
		log.Error("cannot send gossip local state", zap.Error(err))
		return nil
	}
	return b.Bytes()
}

func (g *Gossip) MergeRemoteState(buf []byte, join bool) {
	if len(buf) == 0 {
		return
	}
	var n statusMsg
	if err := gob.NewDecoder(bytes.NewReader(buf)).Decode(&n); err != nil {
		log.Error("cannot merge gossip remote state", zap.Error(err))
		return
	}
	if g.nodes[n.Name] != n.Status {
		g.mu.Lock()
		g.nodes[n.Name] = n.Status
		g.mu.Unlock()
	}
}
