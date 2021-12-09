package etcdserver

import (
	"io"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3/snapshot"
	"go.etcd.io/etcd/embed"
	"go.etcd.io/etcd/mvcc/backend"

	"github.com/criticalstack/e2d/pkg/log"
)

func (s *Server) CreateSnapshot(minRevision int64) (io.ReadCloser, int64, int64, error) {
	// Get the current revision and compare with the minimum requested revision.
	revision := s.Etcd.Server.KV().Rev()
	if revision <= minRevision {
		return nil, 0, revision, errors.Errorf("member revision too old, wanted %d, received: %d", minRevision, revision)
	}
	sp := s.Etcd.Server.Backend().Snapshot()
	if sp == nil {
		return nil, 0, revision, errors.New("no snappy")
	}
	return newSnapshotReadCloser(sp), sp.Size(), revision, nil
}

func newSnapshotReadCloser(snapshot backend.Snapshot) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		n, err := snapshot.WriteTo(pw)
		if err == nil {
			log.Infof("wrote database snapshot out [total bytes: %d]", n)
		}
		_ = pw.CloseWithError(err)
		snapshot.Close()
	}()
	return pr
}

func (s *Server) RestoreSnapshot(snapshotFilename string, peers []*Peer) error {
	if err := validatePeers(peers, s.RequiredClusterSize); err != nil {
		return err
	}
	snapshotMgr := snapshot.NewV3(log.NewLoggerWithLevel("etcd", s.LogLevel))
	return snapshotMgr.Restore(snapshot.RestoreConfig{
		// SnapshotPath is the path of snapshot file to restore from.
		SnapshotPath: snapshotFilename,

		// Name is the human-readable name of this member.
		Name: s.Name,

		// OutputDataDir is the target data directory to save restored data.
		// OutputDataDir should not conflict with existing etcd data directory.
		// If OutputDataDir already exists, it will return an error to prevent
		// unintended data directory overwrites.
		// If empty, defaults to "[Name].etcd" if not given.
		OutputDataDir: s.DataDir,

		// PeerURLs is a list of member's peer URLs to advertise to the rest of the cluster.
		PeerURLs: []string{s.PeerURL.String()},

		// InitialCluster is the initial cluster configuration for restore bootstrap.
		InitialCluster: initialClusterStringFromPeers(peers),

		InitialClusterToken: embed.NewConfig().InitialClusterToken,
		SkipHashCheck:       true,
	})
}
