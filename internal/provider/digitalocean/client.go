package digitalocean

import (
	"context"

	meta "github.com/digitalocean/go-metadata"
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"

	netutil "github.com/criticalstack/e2d/pkg/util/net"
)

type Config struct {
	AccessToken     string
	SpacesAccessKey string
	SpacesSecretKey string
}

func (cfg *Config) Token() (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: cfg.AccessToken}, nil
}

type Client struct {
	*godo.Client
}

func NewClient(cfg *Config) (*Client, error) {
	c := &Client{Client: godo.NewClient(oauth2.NewClient(context.TODO(), cfg))}
	return c, nil
}

func (c *Client) GetAddrsByTag(ctx context.Context, tag string) ([]string, error) {
	metadata, err := meta.NewClient().Metadata()
	if err != nil {
		return nil, err
	}
	droplets, _, err := c.Droplets.ListByTag(ctx, tag, nil)
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
