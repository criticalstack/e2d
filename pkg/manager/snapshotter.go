package manager

import (
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/snapshot"
	snapshotutil "github.com/criticalstack/e2d/pkg/snapshot/util"
)

func (m *Manager) runSnapshotter() {
	if m.snapshotter == nil {
		log.Info("snapshotting disabled: no snapshot backup set")
		return
	}
	log.Debug("starting snapshotter")
	ticker := time.NewTicker(m.cfg.SnapshotConfiguration.Interval.Duration)
	defer ticker.Stop()

	var latestRev int64

	for {
		select {
		case <-ticker.C:
			if m.etcd.IsRestarting() {
				log.Debug("server is restarting, skipping snapshot backup")
				continue
			}
			if !m.etcd.IsLeader() {
				log.Debug("not leader, skipping snapshot backup")
				continue
			}
			log.Debug("starting snapshot backup")
			snapshotData, snapshotSize, rev, err := m.etcd.CreateSnapshot(latestRev)
			if err != nil {
				log.Debug("cannot create snapshot",
					zap.String("name", shortName(m.etcd.Name)),
					zap.Error(err),
				)
				continue
			}
			if m.cfg.SnapshotConfiguration.Encryption {
				snapshotData = snapshotutil.NewEncrypterReadCloser(snapshotData, m.snapshotEncryptionKey, snapshotSize)
			}
			if m.cfg.SnapshotConfiguration.Compression {
				snapshotData = snapshotutil.NewGzipReadCloser(snapshotData)
			}
			if err := m.snapshotter.Save(snapshotData); err != nil {
				log.Debug("cannot save snapshot",
					zap.String("name", shortName(m.etcd.Name)),
					zap.Error(err),
				)
				continue
			}
			latestRev = rev
			log.Infof("wrote snapshot (rev %d) to backup", latestRev)
		case <-m.ctx.Done():
			log.Debug("stopping snapshotter")
			return
		}
	}
}

func getSnapshotProvider(cfg configv1alpha1.SnapshotConfiguration) (snapshot.Snapshotter, error) {
	if cfg.File == "" {
		return nil, nil
	}
	u, err := snapshot.ParseSnapshotBackupURL(cfg.File)
	if err != nil {
		return nil, err
	}

	switch u.Type {
	case snapshot.FileType:
		return snapshot.NewFileSnapshotter(u.Path)
	case snapshot.S3Type:
		awscfg := &snapshot.AmazonConfig{
			Bucket: u.Bucket,
			Key:    u.Path,
		}
		if v, ok := cfg.ExtraArgs["RoleSessionName"]; ok {
			awscfg.RoleSessionName = v
		}
		return snapshot.NewAmazonSnapshotter(awscfg)
	case snapshot.SpacesType:
		return snapshot.NewDigitalOceanSnapshotter(&snapshot.DigitalOceanConfig{
			SpacesURL: cfg.File,
			//SpacesAccessKey: opts.DOSpacesKey,
			//SpacesSecretKey: opts.DOSpacesSecret,
		})
	default:
		return nil, errors.Errorf("unsupported snapshot url format: %#v", cfg.File)
	}
}
