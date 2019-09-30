package discovery

import (
	"context"

	"github.com/criticalstack/e2d/pkg/provider/digitalocean"
)

type DigitalOceanConfig struct {
	AccessToken string
	TagValue    string
}

type DigitalOceanPeerGetter struct {
	*digitalocean.Client
	cfg *DigitalOceanConfig
}

func NewDigitalOceanPeerGetter(cfg *DigitalOceanConfig) (*DigitalOceanPeerGetter, error) {
	client, err := digitalocean.NewClient(&digitalocean.Config{
		AccessToken: cfg.AccessToken,
	})
	if err != nil {
		return nil, err
	}
	return &DigitalOceanPeerGetter{client, cfg}, nil
}

func (p *DigitalOceanPeerGetter) GetAddrs(ctx context.Context) ([]string, error) {
	return p.GetAddrsByTag(ctx, p.cfg.TagValue)
}
