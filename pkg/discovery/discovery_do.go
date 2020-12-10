package discovery

import (
	"context"
	"os"

	"github.com/criticalstack/e2d/internal/provider/digitalocean"
)

type DigitalOceanConfig struct {
	TagValue string
}

type DigitalOceanPeerGetter struct {
	*digitalocean.Client
	cfg *DigitalOceanConfig
}

func NewDigitalOceanPeerGetter(cfg *DigitalOceanConfig) (*DigitalOceanPeerGetter, error) {
	client, err := digitalocean.NewClient(&digitalocean.Config{
		AccessToken:     os.Getenv("ACCESS_TOKEN"),
		SpacesAccessKey: os.Getenv("SPACES_ACCESS_KEY"),
		SpacesSecretKey: os.Getenv("SPACES_SECRET_KEY"),
	})
	if err != nil {
		return nil, err
	}
	return &DigitalOceanPeerGetter{Client: client, cfg: cfg}, nil
}

func (p *DigitalOceanPeerGetter) GetAddrs(ctx context.Context) ([]string, error) {
	return p.GetAddrsByTag(ctx, p.cfg.TagValue)
}
