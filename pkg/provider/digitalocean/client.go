package digitalocean

import (
	"context"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/criticalstack/e2d/pkg/netutil"
	meta "github.com/digitalocean/go-metadata"
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

const (
	tagPrefix = "e2d"
)

type Config struct {
	AccessToken     string
	SpacesURL       string
	SpacesAccessKey string
	SpacesSecretKey string
	SpaceName       string
}

func (cfg *Config) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken: cfg.AccessToken,
	}, nil
}

type Client struct {
	*godo.Client
	store *ObjectStore
}

func NewClient(cfg *Config) (*Client, error) {
	oauthClient := oauth2.NewClient(context.TODO(), cfg)
	c := &Client{Client: godo.NewClient(oauthClient)}
	if cfg.SpaceName != "" {
		var err error
		c.store, err = NewObjectStore(cfg, cfg.SpaceName)
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) GetAddrs(ctx context.Context) ([]string, error) {
	metadata, err := meta.NewClient().Metadata()
	if err != nil {
		return nil, err
	}
	filter := ""
	for _, t := range metadata.Tags {
		if strings.HasPrefix(t, tagPrefix) {
			filter = t
		}
	}
	droplets, _, err := c.Droplets.ListByTag(ctx, filter, nil)
	if err != nil {
		return nil, err
	}
	addrs := make([]string, 0)
	for _, d := range droplets {
		if d.ID == metadata.DropletID {
			continue
		}
		addr, err := d.PrivateIPv4()
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
