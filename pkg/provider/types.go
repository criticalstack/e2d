package provider

import (
	"context"

	"github.com/pkg/errors"
)

var ErrNoProvider = errors.New("no provider")

type Provider interface {
	GetPeerAddresses(ctx context.Context) ([]string, error)
}
