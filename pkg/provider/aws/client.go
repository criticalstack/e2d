package aws

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ec2metadata"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/s3manager"
	"github.com/criticalstack/e2d/pkg/netutil"
)

type Config struct {
	BucketName      string
	RoleSessionName string
}

// Client implements the provider.Client interface.
type Client struct {
	*ec2metadata.Client
	*ec2Client
	*asgClient
	store *ObjectStore
}

// NewClient returns a new Client.
func NewClient(cfg *Config) (*Client, error) {
	awscfg, err := NewConfig()
	if err != nil {
		return nil, err
	}

	if cfg.RoleSessionName != "" {
		awscfg, err = NewConfigWithAssumedRole(cfg.RoleSessionName)
		if err != nil {
			return nil, err
		}
	}
	c := &Client{
		Client: ec2metadata.New(awscfg),
		ec2Client: &ec2Client{
			Client: ec2.New(awscfg),
		},
		asgClient: &asgClient{
			Client: autoscaling.New(awscfg),
		},
	}
	if cfg.BucketName != "" {
		c.store, err = NewObjectStore(awscfg, cfg.BucketName)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) GetAddrs(ctx context.Context) ([]string, error) {
	doc, err := c.GetInstanceIdentityDocument()
	if err != nil {
		return nil, err
	}
	groupName, err := c.describeAutoScalingInstances(ctx, doc.InstanceID)
	if err != nil {
		return nil, err
	}
	instances, err := c.describeAutoScalingGroupsInstances(ctx, groupName)
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0)
	for _, i := range instances {
		if i == doc.InstanceID {
			continue
		}
		addr, err := c.describeInstanceIPAddress(ctx, i)
		if err != nil {
			return nil, err
		}
		if !netutil.IsRoutableIPv4(addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func (c *Client) DownloadFile(ctx context.Context, key string, w io.WriterAt) error {
	_, err := c.store.Downloader.DownloadWithContext(ctx, w, &s3.GetObjectInput{
		Bucket: aws.String(c.store.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (c *Client) UploadFile(ctx context.Context, key string, r io.ReadCloser) error {
	_, err := c.store.Uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(c.store.bucket),
		Key:    aws.String(key),
		Body:   r,
	})
	return err
}
