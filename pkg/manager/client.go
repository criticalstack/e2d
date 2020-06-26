package manager

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/etcdserver/api/v3rpc/rpctypes"

	"github.com/criticalstack/e2d/pkg/client"
)

type Client struct {
	*client.Client

	cfg *client.Config
}

func newClient(cfg *client.Config) (*Client, error) {
	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{c, cfg}, nil
}

func (c *Client) members(ctx context.Context) (map[string]*Member, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	resp, err := c.MemberList(ctx)
	if err != nil {
		return nil, err
	}

	members := make(map[string]*Member)
	for _, member := range resp.Members {
		m := &Member{
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

func (c *Client) addMember(ctx context.Context, peerURL string) (*Member, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	resp, err := c.MemberAdd(ctx, []string{peerURL})
	if err != nil {
		return nil, err
	}
	m := &Member{
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
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	if _, err := c.MemberRemove(ctx, id); err != nil && err != rpctypes.ErrMemberNotFound {
		return errors.Wrap(err, "RemoveMember")
	}
	return nil
}

func (c *Client) removeMemberLocked(ctx context.Context, member *Member) error {
	unlock, err := c.Lock(member.Name, 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	return c.removeMember(ctx, member.ID)
}
