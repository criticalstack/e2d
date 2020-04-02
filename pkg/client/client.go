package client

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"strconv"
	"time"

	"github.com/criticalstack/e2d/pkg/log"
	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/clientv3/concurrency"
	"go.etcd.io/etcd/etcdserver/api/v3rpc/rpctypes"
	"go.etcd.io/etcd/mvcc/mvccpb"
	"go.uber.org/zap"
)

var (
	ErrKeyNotFound = errors.New("key not found")
)

type Client struct {
	*clientv3.Client
	cfg *Config
}

func New(cfg *Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	if !cfg.SecurityConfig.TLSInfo().Empty() {
		var err error
		tlsConfig, err = cfg.SecurityConfig.TLSInfo().ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:        cfg.ClientURLs,
		DialTimeout:      cfg.Timeout,
		TLS:              tlsConfig,
		AutoSyncInterval: cfg.AutoSyncInterval,
		LogConfig: &zap.Config{
			Level:         zap.NewAtomicLevelAt(zap.ErrorLevel),
			Encoding:      "logfmt",
			EncoderConfig: log.NewDefaultEncoderConfig(),
			OutputPaths:   []string{"/dev/null"},
		},
	})
	if err != nil {
		return nil, err
	}
	c := &Client{
		Client: client,
		cfg:    cfg,
	}
	return c, nil
}

func (c *Client) get(key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
	defer cancel()
	resp, err := c.Client.Get(ctx, key, opts...)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) == 0 {
		return resp, errors.Wrap(ErrKeyNotFound, key)
	}
	return resp, nil
}

func (c *Client) Get(key string) ([]byte, error) {
	resp, err := c.get(key)
	if err != nil {
		return nil, err
	}
	return resp.Kvs[0].Value, nil
}

func (c *Client) GetN(key string) (int64, error) {
	resp, err := c.get(key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(resp.Kvs[0].Value), 10, 64)
}

// MustGet blocks until the context is cancelled, an error is received, or
// the key is present. Returns the value of the key once present.
func (c *Client) MustGet(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := c.Client.Watch(ctx, key)
	if v, err := c.Get(key); err == nil {
		return v, nil
	}

	for {
		select {
		case r := <-ch:
			// etcd will send a Canceled message on a watch before
			// closing the channel, so we check that first to see if the
			// watch is closed and return the associated error
			if r.Canceled {
				return nil, r.Err()
			}

			// indicates that this is a "ProgressUpdate"
			if len(r.Events) == 0 {
				continue
			}
			return r.Events[0].Kv.Value, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Client) Set(key, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
	defer cancel()
	_, err := c.Client.Put(ctx, key, value)
	return err
}

func (c *Client) SetOnce(ctx context.Context, key, value string) (bool, error) {
	resp, err := c.Client.Txn(ctx).If(
		clientv3.Compare(clientv3.Version(key), "=", 0),
	).Then(
		clientv3.OpPut(key, value, clientv3.WithLease(clientv3.NoLease)),
	).Commit()
	if err != nil {
		return false, err
	}
	return resp.Succeeded, nil
}

func (c *Client) Count(key string) (int64, error) {
	resp, err := c.get(key, clientv3.WithPrefix(), clientv3.WithCountOnly())
	if err != nil && errors.Cause(err) != ErrKeyNotFound {
		return 0, err
	}
	return resp.Count, nil
}

func (c *Client) Exists(key string) (bool, error) {
	n, err := c.Count(key)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *Client) Prefix(key string) ([]*mvccpb.KeyValue, error) {
	resp, err := c.get(key, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	return resp.Kvs, nil
}

func (c *Client) Lock(key string, timeout time.Duration) (context.CancelFunc, error) {
	// The session uses a low TTL to ensure that keep alives are sent more
	// frequently than the default. This ensures that a failed node with
	// initiated locks will not cause a deadlock for more than 5 seconds.
	session, err := concurrency.NewSession(c.Client, concurrency.WithTTL(5))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	mutex := concurrency.NewMutex(session, filepath.Join(key, "lock"))
	if err := mutex.Lock(ctx); err != nil {
		session.Close()
		return nil, err
	}
	unlock := func() {
		if err := mutex.Unlock(ctx); err != nil {
			log.Debug("mutex.Unlock", zap.Error(err))
		}
		session.Close()
	}
	return unlock, nil
}

func (c *Client) Incr(key string, timeout time.Duration) (int64, error) {
	unlock, err := c.Lock(key, timeout)
	if err != nil {
		return 0, err
	}
	defer unlock()
	id, err := c.GetN(key)
	if err != nil && errors.Cause(err) != ErrKeyNotFound {
		return 0, err
	}
	id++
	if err := c.Set(key, strconv.FormatInt(id, 10)); err != nil {
		return 0, err
	}
	return id, nil
}

func (c *Client) IsHealthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	_, err := c.Client.Get(ctx, "health", clientv3.WithSerializable())
	if err == nil || err == rpctypes.ErrPermissionDenied || err == rpctypes.ErrGRPCCompacted {
		return nil
	}
	return err
}
