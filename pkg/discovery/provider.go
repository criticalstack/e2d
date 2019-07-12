package discovery

import "context"

type PeerProvider interface {
	GetAddrs(context.Context) ([]string, error)
}

type NoopProvider struct{}

func (*NoopProvider) GetAddrs(ctx context.Context) ([]string, error) {
	return []string{}, nil
}
