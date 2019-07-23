package e2db

import (
	"context"
	"fmt"
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

// XXX(chris):
func (db *DB) DumpKeys() error {
	kvs, err := db.client.Prefix("/")
	if err != nil {
		return err
	}
	for _, kv := range kvs {
		fmt.Printf("%s\n", kv.Key)
	}
	return nil
}

func (db *DB) Lock(name string, timeout time.Duration) (context.CancelFunc, error) {
	return db.client.Lock(name, timeout)
}

func (db *DB) Table(iface interface{}) *Table {
	t := &Table{
		db:   db,
		c:    &gobCodec{},
		meta: NewModelDef(reflect.TypeOf(iface)),
	}
	return t
}
