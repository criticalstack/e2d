package etcdserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

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
