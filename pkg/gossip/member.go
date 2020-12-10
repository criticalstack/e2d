package gossip

import (
	"bytes"
	"encoding/gob"

	"github.com/hashicorp/memberlist"
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
