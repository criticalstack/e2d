package discovery

import (
	"context"
)

type PeerGetter interface {
	GetAddrs(context.Context) ([]string, error)
}

type NoopGetter struct{}

func (*NoopGetter) GetAddrs(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

type KeyValue struct {
	Key, Value string
}
