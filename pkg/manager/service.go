package manager

import (
	"context"

	"github.com/gogo/protobuf/types"
	"go.uber.org/zap"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/e2db"
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
		ClientAddr: s.m.cfg.ClientURL.String(),
		CAFile:     s.m.cfg.PeerSecurity.TrustedCAFile,
		CertFile:   s.m.cfg.PeerSecurity.CertFile,
		KeyFile:    s.m.cfg.PeerSecurity.KeyFile,
		Namespace:  string(volatilePrefix),
	})
	if err != nil {
		return resp, err
	}
	defer db.Close()

	var cluster *Cluster
	if err := db.Table(new(Cluster)).Find("ID", 1, &cluster); err != nil {
		return resp, err
	}
	c, err := client.New(&client.Config{
		ClientURLs:     []string{s.m.cfg.ClientURL.String()},
		SecurityConfig: s.m.cfg.PeerSecurity,
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
	if s.m.etcd.isRestarting() {
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
