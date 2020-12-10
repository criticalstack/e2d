package manager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/criticalstack/e2d/pkg/etcdserver"
	"github.com/criticalstack/e2d/pkg/gossip"
	"github.com/criticalstack/e2d/pkg/log"
)

func (m *Manager) runMembershipCleanup() {
	if m.cfg.RequiredClusterSize == 1 {
		return
	}

	membership := newMembership(m.ctx, m.cfg.HealthCheckTimeout.Duration, func(name string) error {
		log.Debug("removing member ...",
			zap.String("name", shortName(m.etcd.Name)),
			zap.String("removed", shortName(name)),
		)
		if err := m.etcd.RemoveMember(m.ctx, name); err != nil && errors.Cause(err) != etcdserver.ErrCannotFindMember {
			return err
		}
		log.Debug("member removed",
			zap.String("name", shortName(m.etcd.Name)),
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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// When this member's gossip network does not have enough members
			// to be considered a majority, it is no longer eligible to affect
			// cluster membership. This helps ensure that when a network
			// partition takes place that minority partition(s) will not
			// attempt to change cluster membership. Only members in Running
			// status are considered.
			if membership.ensureQuorum(len(m.gossip.RunningMembers()) > m.cfg.RequiredClusterSize/2) {
				continue
			}
			members := make([]string, 0)
			for _, m := range m.gossip.Members() {
				members = append(members, fmt.Sprintf("%s=%s", m.Name, m.Status))
			}
			suspects := make([]string, 0)
			for k, v := range membership.suspects {
				suspects = append(suspects, fmt.Sprintf("%s=%s", k, v.Format(time.RFC3339)))
			}
			log.Debug("not enough members are healthy to remove other members",
				zap.String("name", shortName(m.etcd.Name)),
				zap.Int("gossip-members-running", len(m.gossip.RunningMembers())),
				zap.String("gossip-members", strings.Join(members, ",")),
				zap.Int("required-cluster-size", m.cfg.RequiredClusterSize),
				zap.Bool("hasQuorum", membership.hasQuorum),
				zap.String("suspects", strings.Join(suspects, ",")),
			)
		case ev := <-m.gossip.Events():
			// It is possible to receive an event from memberlist where the
			// Node is nil. This most likely happens when starting and stopping
			// the server quickly, so is mostly observed during testing.
			if ev.Node == nil {
				log.Debug("discarded null event")
				continue
			}
			member := &gossip.Member{}
			if err := member.Unmarshal(ev.Node.Meta); err != nil {
				log.Debugf("[%v]: cannot unmarshal node meta: %v", shortName(m.etcd.Name), err)
				continue
			}
			log.Debug("received membership event",
				zap.String("name", shortName(m.etcd.Name)),
				zap.Int("event-type", int(ev.Event)),
				zap.Uint64("member-id", member.ID),
				zap.String("member-name", member.Name),
				zap.String("member-client-addr", member.ClientAddr),
				zap.String("member-peer-addr", member.PeerAddr),
				zap.String("member-gossip-addr", member.GossipAddr),
			)

			// This member must not acknowledge membership changes related to
			// itself. Gossip events are used to determine when a member needs
			// to be evicted, and this responsibility falls to peers only (i.e.
			// a member should never evict itself). The PeerURL is used rather
			// than the name or gossip address as it better represents a
			// distinct member of the cluster as only one PeerURL will ever be
			// present on a network.
			if member.PeerURL == m.etcd.PeerURL.String() {
				continue
			}
			switch ev.Event {
			case memberlist.NodeJoin:
				log.Debugf("[%v]: member joined: %#v", shortName(m.etcd.Name), member.Name)

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
				if oldName, err := m.etcd.LookupMemberNameByPeerAddr(member.PeerURL); err == nil {
					log.Debugf("[%v]: member %v peerAddr %q in use by member %v", shortName(m.etcd.Name), member.Name, member.PeerURL, oldName)
					if oldName != member.Name {
						log.Debugf("[%v]: members name mismatched, evicting %v", shortName(m.etcd.Name), oldName)
						if err := membership.removeMember(oldName); err != nil {
							log.Debug("unable to remove member", zap.Error(err))
						}
					}
				}

				membership.removeSuspect(member.Name)
			case memberlist.NodeLeave:
				membership.addSuspect(member.Name)
			case memberlist.NodeUpdate:
			}
		case <-m.ctx.Done():
			return
		}
	}
}

type removerFunc func(string) error

type membership struct {
	timeout time.Duration
	fn      removerFunc

	mu        sync.RWMutex
	suspects  map[string]time.Time
	hasQuorum bool
}

func newMembership(ctx context.Context, d time.Duration, fn removerFunc) *membership {
	c := &membership{
		timeout:  d,
		fn:       fn,
		suspects: make(map[string]time.Time),
	}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for name, t := range c.suspects {
					// check if the node has been evicted past the health
					// timeout before proceeding to remove
					if t.Add(c.timeout).After(time.Now()) {
						continue
					}
					if err := c.removeMember(name); err != nil {
						log.Debug("cannot remove member", zap.Error(err))
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return c
}

func (c *membership) addSuspect(name string) {
	c.mu.Lock()
	c.suspects[name] = time.Now()
	c.mu.Unlock()
}

func (c *membership) removeSuspect(name string) {
	c.mu.Lock()
	delete(c.suspects, name)
	c.mu.Unlock()
}

func (c *membership) removeMember(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.hasQuorum {
		log.Debug("gossip network lost quorum, cannot remove member",
			zap.String("name", name),
		)
		return nil
	}
	if err := c.fn(name); err != nil {
		return err
	}
	delete(c.suspects, name)
	return nil
}

func (c *membership) ensureQuorum(q bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// quorum has not changed, so no further actions taken
	if q == c.hasQuorum {
		return c.hasQuorum
	}
	c.hasQuorum = q
	if c.suspects == nil {
		c.suspects = make(map[string]time.Time)
	}
	for name := range c.suspects {
		c.suspects[name] = time.Now()
	}
	return c.hasQuorum
}
