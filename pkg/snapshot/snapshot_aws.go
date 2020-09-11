package snapshot

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"

	e2daws "github.com/criticalstack/e2d/internal/provider/aws"
)

func newAWSConfig(name string) (*aws.Config, error) {
	if name != "" {
		return e2daws.NewConfigWithRoleSession(name)
	}
	return e2daws.NewConfig()
}

type AmazonConfig struct {
	RoleSessionName string
	Bucket          string
	Key             string
}

type AmazonSnapshotter struct {
	*s3.S3
	*s3manager.Downloader
	*s3manager.Uploader

	bucket, key string
}

func NewAmazonSnapshotter(cfg *AmazonConfig) (*AmazonSnapshotter, error) {
	awsCfg, err := newAWSConfig(cfg.RoleSessionName)
	if err != nil {
		return nil, err
	}
	return newAmazonSnapshotter(awsCfg, cfg.Bucket, cfg.Key)
}

func newAmazonSnapshotter(cfg *aws.Config, bucket, key string) (*AmazonSnapshotter, error) {
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, err
	}
	s := &AmazonSnapshotter{
		S3:         s3.New(sess),
		Downloader: s3manager.NewDownloader(sess),
		Uploader:   s3manager.NewUploader(sess),
		bucket:     bucket,
		key:        key,
	}

	// Ensure that the bucket exists
	req, _ := s.HeadBucketRequest(&s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	err = req.Send()
	if err != nil {
		if reqErr, ok := err.(awserr.RequestFailure); ok {
			switch reqErr.StatusCode() {
			case http.StatusNotFound:
				return nil, errors.Errorf("bucket %s does not exist", bucket)
			case http.StatusForbidden:
				return nil, errors.Errorf("access to bucket %s forbidden", bucket)
			default:
				return nil, errors.Errorf("bucket could not be accessed: %v", err)
			}
		}
	}
	return s, nil
}

func (s *AmazonSnapshotter) Load() (io.ReadCloser, error) {
	tmpFile, err := ioutil.TempFile("", "snapshot.download")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	if _, err = s.DownloadWithContext(ctx, tmpFile, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	}); err != nil {
		tmpFile.Close()
		return nil, errors.Wrapf(err, "cannot download file: %v", s.key)
	}
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return nil, err
	}
	return tmpFile, nil
}

func (s *AmazonSnapshotter) Save(r io.ReadCloser) error {
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	_, err := s.UploadWithContext(ctx, &s3manager.UploadInput{
		Body:   r,
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	return err
}
