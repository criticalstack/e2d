package snapshot

import (
	"context"
	"io"
	"io/ioutil"

	"github.com/criticalstack/e2d/pkg/provider/digitalocean"
	"github.com/pkg/errors"
)

type DigitalOceanSnapshotter struct {
	client *digitalocean.Client
	key    string
}

func NewDigitalOceanSnapshotter(cfg *digitalocean.Config) (*DigitalOceanSnapshotter, error) {
	client, err := digitalocean.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &DigitalOceanSnapshotter{client: client, key: "etcd.snapshot"}, nil
}

func (s *DigitalOceanSnapshotter) Load() (io.ReadCloser, error) {
	tmpFile, err := ioutil.TempFile("", "snapshot.download")
	if err != nil {
		return nil, err
	}
	if err := s.client.DownloadFile(context.TODO(), s.key, tmpFile); err != nil {
		tmpFile.Close()
		return nil, errors.Wrapf(err, "cannot download file: %v", s.key)
	}
	tmpFile.Seek(0, 0)
	return tmpFile, nil
}

func (s *DigitalOceanSnapshotter) Save(r io.ReadCloser) error {
	defer r.Close()
	return s.client.UploadFile(context.TODO(), s.key, r)
}
