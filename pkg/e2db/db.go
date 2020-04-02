package e2db

import (
	"context"
	"crypto/sha512"
	"reflect"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3/namespace"
)

type DB struct {
	client *client.Client
	cfg    *Config
}

func New(cfg *Config) (*DB, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	c, err := client.New(&client.Config{
		ClientURLs:       []string{cfg.clientURL.String()},
		SecurityConfig:   cfg.securityConfig,
		AutoSyncInterval: cfg.AutoSyncInterval,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	if _, err := c.MemberList(ctx); err != nil {
		return nil, errors.Wrapf(err, "cannot perform request, database may be unavailable: %s", cfg.clientURL.String())
	}

	if cfg.Namespace != "" {
		c.KV = namespace.NewKV(c.KV, "/"+cfg.Namespace)
		c.Watcher = namespace.NewWatcher(c.Watcher, "/"+cfg.Namespace)
		c.Lease = namespace.NewLease(c.Lease, "/"+cfg.Namespace)
	}

	db := &DB{
		client: c,
		cfg:    cfg,
	}
	return db, nil
}

func (db *DB) Close() {
	db.client.Close()
}

func (db *DB) Lock(name string, timeout time.Duration) (context.CancelFunc, error) {
	return db.client.Lock(name, timeout)
}

type TableOptions struct {
	Encrypted bool
}

type TableOption func(*Table)

func WithEncryption(secretKey []byte) TableOption {
	return func(t *Table) {
		key := [32]byte{}
		copy(key[:], sha512.New512_256().Sum(secretKey))
		t.c = &encryptedGobCodec{key: &key}
	}
}

func (db *DB) Table(iface interface{}, options ...TableOption) *Table {
	t := &Table{
		db:   db,
		c:    &gobCodec{},
		tc:   &gobCodec{},
		meta: NewModelDef(reflect.TypeOf(iface)),
	}
	for _, opt := range options {
		opt(t)
	}
	return t
}
