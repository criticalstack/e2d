package etcdserver

import (
	"bytes"
	"context"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/lease"
	"go.etcd.io/etcd/mvcc"

	"github.com/criticalstack/e2d/pkg/e2db"
)

// move to manager?
type Cluster struct {
	ID                  int `e2db:"id"`
	Created             time.Time
	RequiredClusterSize int
}

// writeClusterInfo attempts to write basic cluster info whenever a server
// starts or joins a new cluster. The e2db namespace matches the volatile
// prefix so that this information will not survive being restored from
// snapshot. This is because the cluster requirements could change for the
// restored cluster (e.g. going from RequiredClusterSize 1 -> 3).
func (s *Server) writeClusterInfo(ctx context.Context) error {
	// NOTE(chrism): As the naming can be confusing it is worth pointing out
	// that the ClientSecurity field is specifying the server certs and NOT the
	// client certs. Since the server certs do not have client auth key usage,
	// we need to use the peer certs here (they have client auth key usage).
	db, err := e2db.New(ctx, &e2db.Config{
		ClientAddr: s.ClientURL.String(),
		CAFile:     s.PeerSecurity.TrustedCAFile,
		CertFile:   s.PeerSecurity.CertFile,
		KeyFile:    s.PeerSecurity.KeyFile,
		Namespace:  string(VolatilePrefix),
	})
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Table(new(Cluster)).Tx(func(tx *e2db.Tx) error {
		var cluster *Cluster
		if err := tx.Find("ID", 1, &cluster); err != nil && errors.Cause(err) != e2db.ErrNoRows {
			return err
		}

		if cluster != nil {
			// check RequiredClusterSize for discrepancies
			if cluster.RequiredClusterSize != s.RequiredClusterSize {
				return errors.Errorf("server %s attempted to join cluster with incorrect RequiredClusterSize, cluster expects %d, this server is configured with %d", s.Name, cluster.RequiredClusterSize, s.RequiredClusterSize)
			}
			return nil
		}

		return tx.Insert(&Cluster{
			ID:                  1,
			Created:             time.Now(),
			RequiredClusterSize: s.RequiredClusterSize,
		})
	})
}

var (
	// VolatilePrefix is the key prefix used for keys that will NOT be
	// preserved after a cluster is recovered from snapshot
	VolatilePrefix = []byte("/_e2d")

	// SnapshotMarkerKey is the key used to indicate when a cluster recovered
	// from snapshot
	SnapshotMarkerKey = []byte("/_e2d/snapshot")
)

var ErrServerStopped = errors.New("server stopped")

func (s *Server) ClearVolatilePrefix() (rev, deleted int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return 0, 0, ErrServerStopped
	}
	res, err := s.Server.KV().Range(VolatilePrefix, []byte{}, mvcc.RangeOptions{})
	if err != nil {
		return 0, 0, err
	}
	for _, kv := range res.KVs {
		if bytes.HasPrefix(kv.Key, VolatilePrefix) {
			n, _ := s.Server.KV().DeleteRange(kv.Key, nil)
			deleted += n
		}
	}
	return res.Rev, deleted, nil
}

func (s *Server) PlaceSnapshotMarker(v []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return 0, ErrServerStopped
	}
	rev := s.Server.KV().Put(SnapshotMarkerKey, v, lease.NoLease)
	return rev, nil
}
