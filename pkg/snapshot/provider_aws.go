package snapshot

import (
	"context"
	"io"
	"io/ioutil"

	"github.com/criticalstack/e2d/pkg/provider/aws"
	"github.com/pkg/errors"
)

type AWSSnapshotter struct {
	client *aws.Client
	key    string
}

func NewAWSSnapshotter(cfg *aws.Config) (*AWSSnapshotter, error) {
	client, err := aws.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &AWSSnapshotter{client: client, key: "etcd.snapshot"}, nil
}

func (s *AWSSnapshotter) Load() (io.ReadCloser, error) {
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

func (s *AWSSnapshotter) Save(r io.ReadCloser) error {
	defer r.Close()
	return s.client.UploadFile(context.TODO(), s.key, r)
}
