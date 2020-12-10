package etcdserver

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/etcdserver/api/membership"
)

var (
	ErrCannotFindMember = errors.New("cannot find member")
)

func (s *Server) LookupMemberNameByPeerAddr(addr string) (string, error) {
	for _, member := range s.Etcd.Server.Cluster().Members() {
		for _, url := range member.PeerURLs {
			if url == addr {
				return member.Name, nil
			}
		}
	}
	return "", errors.Wrap(ErrCannotFindMember, addr)
}

func (s *Server) RemoveMember(ctx context.Context, name string) error {
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

func (s *Server) lookupMember(name string) (uint64, error) {
	for _, member := range s.Etcd.Server.Cluster().Members() {
		if member.Name == name {
			return uint64(member.ID), nil
		}
	}
	return 0, errors.Wrap(ErrCannotFindMember, name)
}
