package aws

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/awserr"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/s3manager"
	"github.com/pkg/errors"
)

// ObjectStore implements the provider.ObjectStore interface for AWS S3.
type ObjectStore struct {
	*s3.Client
	*s3manager.Uploader
	*s3manager.Downloader

	bucket string
}

// NewObjectStore returns a new instance of ObjectStore.
func NewObjectStore(cfg aws.Config, bucket string) (*ObjectStore, error) {
	if bucket == "" {
		return nil, errors.New("must provide bucket name")
	}
	client := s3.New(cfg)
	s := &ObjectStore{
		Client:     client,
		Uploader:   s3manager.NewUploaderWithClient(client),
		Downloader: s3manager.NewDownloaderWithClient(client),
		bucket:     bucket,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ensure that the bucket exists
	req := s.HeadBucketRequest(&s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	_, err := req.Send(ctx)
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

// Exists checks for existence of a particular key in the established object
// bucket.
func (s *ObjectStore) Exists(ctx context.Context, key string) (bool, error) {
	req := s.HeadObjectRequest(&s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	_, err := req.Send(ctx)
	if err != nil {
		if reqErr, ok := err.(awserr.RequestFailure); ok {
			if reqErr.StatusCode() == http.StatusNotFound {
				return false, nil
			}
			return false, err
		}
		return false, err
	}
	return true, nil
}

// Download downloads an object given the provided key.
func (s *ObjectStore) Download(ctx context.Context, key string) ([]byte, error) {
	buf := aws.NewWriteAtBuffer([]byte{})
	_, err := s.Downloader.DownloadWithContext(ctx, buf, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "cannot download file: %v", key)
	}
	return buf.Bytes(), nil
}

// Upload uploads an object given the provided key and file content.
func (s *ObjectStore) Upload(ctx context.Context, key string, data []byte) error {
	_, err := s.Uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return errors.Wrapf(err, "cannot upload file: %v", key)
}
