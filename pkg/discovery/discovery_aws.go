package discovery

import (
	"context"

	"github.com/pkg/errors"

	e2daws "github.com/criticalstack/e2d/internal/provider/aws"
)

type AmazonAutoScalingPeerGetter struct {
	*e2daws.Client
}

func NewAmazonAutoScalingPeerGetter() (*AmazonAutoScalingPeerGetter, error) {
	awsCfg, err := e2daws.NewConfig()
	if err != nil {
		return nil, err
	}
	client, err := e2daws.NewClient(awsCfg)
	if err != nil {
		return nil, err
	}
	return &AmazonAutoScalingPeerGetter{client}, nil
}

func (p *AmazonAutoScalingPeerGetter) GetAddrs(ctx context.Context) ([]string, error) {
	return p.GetAutoScalingGroupAddresses(ctx)
}

type AmazonInstanceTagPeerGetter struct {
	*e2daws.Client
	tags map[string]string
}

func NewAmazonInstanceTagPeerGetter(kvs []KeyValue) (*AmazonInstanceTagPeerGetter, error) {
	if len(kvs) == 0 {
		return nil, errors.New("must provide at least 1 tag key/value")
	}
	awsCfg, err := e2daws.NewConfig()
	if err != nil {
		return nil, err
	}
	client, err := e2daws.NewClient(awsCfg)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string)
	for _, kv := range kvs {
		tags[kv.Key] = kv.Value
	}
	return &AmazonInstanceTagPeerGetter{
		Client: client,
		tags:   tags,
	}, nil
}

func (p *AmazonInstanceTagPeerGetter) GetAddrs(ctx context.Context) ([]string, error) {
	return p.GetAddressesByTag(ctx, p.tags)
}
