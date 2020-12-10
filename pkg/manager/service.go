package manager

import (
	"context"

	"github.com/gogo/protobuf/types"
	"go.uber.org/zap"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/e2db"
	"github.com/criticalstack/e2d/pkg/etcdserver"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager/e2dpb"
)

type ManagerService struct {
	m *Manager
}

func (s *ManagerService) Health(ctx context.Context, _ *types.Empty) (*e2dpb.HealthResponse, error) {
	resp := &e2dpb.HealthResponse{
		Status: "not great, bob",
	}
	db, err := e2db.New(ctx, &e2db.Config{
		ClientAddr: s.m.etcd.ClientURL.String(),
		CAFile:     s.m.etcd.PeerSecurity.TrustedCAFile,
		CertFile:   s.m.etcd.PeerSecurity.CertFile,
		KeyFile:    s.m.etcd.PeerSecurity.KeyFile,
		Namespace:  string(etcdserver.VolatilePrefix),
	})
	if err != nil {
		return resp, err
	}
	defer db.Close()

	var cluster *etcdserver.Cluster
	if err := db.Table(new(etcdserver.Cluster)).Find("ID", 1, &cluster); err != nil {
		return resp, err
	}
	c, err := client.New(&client.Config{
		ClientURLs:     []string{s.m.etcd.ClientURL.String()},
		SecurityConfig: s.m.etcd.PeerSecurity,
	})
	if err != nil {
		return resp, err
	}
	if err := c.IsHealthy(ctx); err != nil {
		return resp, err
	}
	cresp, err := c.MemberList(ctx)
	if err != nil {
		return resp, err
	}
	if len(cresp.Members) >= cluster.RequiredClusterSize {
		resp.Status = "It cool"
	}
	return resp, nil
}

func (s *ManagerService) Restart(ctx context.Context, _ *types.Empty) (*e2dpb.RestartResponse, error) {
	resp := &e2dpb.RestartResponse{
		Msg: "attempting restarting ...",
	}
	if s.m.etcd.IsRestarting() {
		resp.Msg = "a restart is already in progress"
		return resp, nil
	}
	go func() {
		if err := s.m.Restart(); err != nil {
			log.Debug("remote restart failed", zap.Error(err))
		}
	}()
	return resp, nil
}
