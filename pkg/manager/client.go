package manager

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/etcdserver/api/v3rpc/rpctypes"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/gossip"
)

type Client struct {
	*client.Client

	Timeout time.Duration
}

func (c *Client) members(ctx context.Context) (map[string]*gossip.Member, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	resp, err := c.MemberList(ctx)
	if err != nil {
		return nil, err
	}

	members := make(map[string]*gossip.Member)
	for _, member := range resp.Members {
		m := &gossip.Member{
			ID:   member.ID,
			Name: member.Name,
		}
		if len(member.ClientURLs) > 0 {
			m.ClientURL = member.ClientURLs[0]
		}
		if len(member.PeerURLs) > 0 {
			m.PeerURL = member.PeerURLs[0]
		}
		members[m.Name] = m
	}
	return members, nil
}

func (c *Client) addMember(ctx context.Context, peerURL string) (*gossip.Member, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	resp, err := c.MemberAdd(ctx, []string{peerURL})
	if err != nil {
		return nil, err
	}
	m := &gossip.Member{
		ID:   resp.Member.ID,
		Name: resp.Member.Name,
	}
	if len(resp.Member.ClientURLs) > 0 {
		m.ClientURL = resp.Member.ClientURLs[0]
	}
	if len(resp.Member.PeerURLs) > 0 {
		m.PeerURL = resp.Member.PeerURLs[0]
	}
	return m, nil
}

func (c *Client) removeMember(ctx context.Context, id uint64) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	if _, err := c.MemberRemove(ctx, id); err != nil && err != rpctypes.ErrMemberNotFound {
		return errors.Wrap(err, "RemoveMember")
	}
	return nil
}

func (c *Client) removeMemberLocked(ctx context.Context, member *gossip.Member) error {
	unlock, err := c.Lock(member.Name, 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	return c.removeMember(ctx, member.ID)
}
