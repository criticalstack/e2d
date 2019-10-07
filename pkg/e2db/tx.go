package e2db

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/e2db/key"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/pkg/errors"
	"go.etcd.io/etcd/clientv3"
	"go.etcd.io/etcd/etcdserver/etcdserverpb"
	"go.uber.org/zap"
)

var (
	ErrFieldRequired     = errors.New("must provide field")
	ErrInvalidPrimaryKey = errors.New("invalid primary key")
	ErrTableNotFound     = errors.New("table not found")
	ErrUniqueConstraint  = errors.New("violates unique constraint")
)

type Tx struct {
	*Table
}

func (t *Table) Tx(fn func(*Tx) error) error {
	if err := t.tableMustExist(); err != nil {
		return err
	}

	// TODO(chris): add a way to set the timeout here
	unlock, err := t.db.client.Lock(key.TableLock(t.meta.Name), t.db.cfg.Timeout)
	if err != nil {
		return err
	}
	defer unlock()

	return fn(&Tx{t})
}

type batchResponse struct {
	Deleted int64
}

func (tx *Tx) batchOps(ops ...clientv3.Op) (*batchResponse, error) {
	resp, err := tx.db.client.Txn(context.TODO()).Then(ops...).Commit()
	if err != nil {
		return nil, err
	}
	br := &batchResponse{}
	for _, r := range resp.Responses {
		switch t := r.Response.(type) {
		case *etcdserverpb.ResponseOp_ResponseRange:
		case *etcdserverpb.ResponseOp_ResponsePut:
		case *etcdserverpb.ResponseOp_ResponseDeleteRange:
			br.Deleted += t.ResponseDeleteRange.Deleted
		case *etcdserverpb.ResponseOp_ResponseTxn:
		}
	}
	return br, err
}

func (tx *Tx) Insert(iface interface{}) error {
	m := NewModelItem(reflect.ValueOf(iface))
	if err := tx.validateModel(m.ModelDef); err != nil {
		return err
	}
	pk, err := m.getPrimaryKey()
	if err != nil {
		return err
	}
	if pk.isZero() {
		if pk.hasTag("increment") {
			id, err := tx.db.client.Incr(key.Increment(m.Name, pk.Name), 5*time.Second)
			if err != nil {
				return err
			}
			switch pk.value.Kind() {
			case reflect.Int:
				pk.value.Set(reflect.ValueOf(int(id)))
			case reflect.Int64:
				pk.value.Set(reflect.ValueOf(id))
			}
		}
	}
	id := toString(pk.value.Interface())
	if id == "" {
		return errors.Wrapf(ErrInvalidPrimaryKey, "cannot be empty: %#v", pk.Name)
	}
	indexes := make([]string, 0)
	for _, f := range m.Fields {
		for _, tag := range f.Tags {
			switch tag.Name {
			case "index":
				indexes = append(indexes, key.Index(m.Name, f.Name, toString(f.value.Interface()), id))
			case "required":
				if f.isZero() {
					return errors.Wrap(ErrFieldRequired, f.Name)
				}
			case "unique":
				k := key.Unique(m.Name, f.Name, toString(f.value.Interface()))
				ok, err := tx.db.client.Exists(k)
				if err != nil {
					return err
				}
				if ok {
					return errors.Wrapf(ErrUniqueConstraint, "%#v: %#v", f.Name, f.value.String())
				}
				indexes = append(indexes, k)
			}
		}
	}
	data, err := tx.c.Encode(iface)
	if err != nil {
		return err
	}
	ops := make([]clientv3.Op, 0)
	ops = append(ops, clientv3.OpPut(key.ID(m.Name, id), string(data)))
	for _, idx := range indexes {
		ops = append(ops, clientv3.OpPut(idx, key.ID(m.Name, id)))
	}
	_, err = tx.batchOps(ops...)
	return err
}

func (tx *Tx) Update(iface interface{}) error {
	v := reflect.Indirect(reflect.ValueOf(iface))
	m := NewModelItem(v)
	if err := tx.validateModel(m.ModelDef); err != nil {
		return err
	}
	pk, err := m.getPrimaryKey()
	if err != nil {
		return err
	}
	id := toString(pk.value.Interface())
	if id == "" {
		return errors.Wrapf(ErrInvalidPrimaryKey, "cannot be empty: %#v", pk.Name)
	}
	dbValue := reflect.Indirect(reflect.New(v.Type()))
	if err := newQuery(tx.Table).findOneByPrimaryKey(key.ID(m.Name, id), dbValue); err != nil {
		if errors.Cause(err) == ErrNoRows {
			return tx.Insert(iface)
		}
		return err
	}
	indexes := make(map[string]string)
	for _, f := range m.Fields {
		if f.Name == pk.Name {
			continue
		}
		dbFieldValue := dbValue.FieldByName(f.Name)
		if reflect.DeepEqual(f.value.Interface(), dbFieldValue.Interface()) {
			continue
		}

		// if field is required and zero-value, it is safe to presume it was
		// already set
		if f.hasTag("required") && f.isZero() {
			continue
		}
		for _, tag := range f.Tags {
			switch tag.Name {
			case "index":
				oldIdx := key.Index(m.Name, f.Name, toString(dbFieldValue.Interface()), id)
				newIdx := key.Index(m.Name, f.Name, toString(f.value.Interface()), id)
				indexes[oldIdx] = newIdx
			case "unique":
				oldIdx := key.Unique(m.Name, f.Name, toString(dbFieldValue.Interface()))
				newIdx := key.Unique(m.Name, f.Name, toString(f.value.Interface()))
				ok, err := tx.db.client.Exists(newIdx)
				if err != nil {
					return err
				}
				if ok {
					return errors.Wrapf(ErrUniqueConstraint, "%#v: %#v", f.Name, f.value.String())
				}
				indexes[oldIdx] = newIdx
			}
		}
		dbFieldValue.Set(f.value)
	}
	data, err := tx.c.Encode(dbValue.Interface())
	if err != nil {
		return err
	}
	ops := make([]clientv3.Op, 0)
	ops = append(ops, clientv3.OpPut(key.ID(m.Name, id), string(data)))
	for oldIdx, newIdx := range indexes {
		ops = append(ops, clientv3.OpDelete(oldIdx))
		ops = append(ops, clientv3.OpPut(newIdx, key.ID(m.Name, id)))
	}
	_, err = tx.batchOps(ops...)
	return err
}

// getIndexesByPrimaryKey returns all index keys for the provided primary key
// based on the stored value.
func (tx *Tx) getIndexesByPrimaryKey(pk string) ([]string, error) {
	val := tx.meta.New()
	if val == nil {
		return nil, errors.Errorf("underlying type is uninitialized: %s", tx.meta.Name)
	}
	v := reflect.Indirect(reflect.ValueOf(val.Interface()))
	if err := newQuery(tx.Table).findOneByPrimaryKey(pk, v); err != nil {
		if errors.Cause(err) == ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	keys := []string{pk}
	_, id := filepath.Split(pk)
	for n, f := range tx.meta.Fields {
		switch f.Type() {
		case UniqueIndex:
			keys = append(keys, key.Unique(tx.meta.Name, n, toString(v.FieldByName(n).Interface())))
		case SecondaryIndex:
			keys = append(keys, key.Index(tx.meta.Name, n, toString(v.FieldByName(n).Interface()), id))
		}
	}
	return keys, nil
}

func deleteOps(keys []string) (ops []clientv3.Op) {
	for _, k := range keys {
		ops = append(ops, clientv3.OpDelete(k))
	}
	return
}

func (tx *Tx) Delete(fieldName string, data interface{}) (int64, error) {
	var n int64
	st := time.Now()
	defer func() {
		log.Debug("tx.Delete",
			zap.String("key", fmt.Sprintf("%s/%v", tx.meta.Name, fieldName)),
			zap.String("q", toString(data)),
			zap.Int64("n", n),
			zap.Duration("elapsed", time.Now().Sub(st)),
		)
	}()
	f, ok := tx.meta.Fields[fieldName]
	if !ok {
		return 0, errors.Errorf("invalid field name: %#v", fieldName)
	}
	k := toString(data)
	pks := make([]string, 0)

	// get the primary key of the item(s) being deleted
	switch f.Type() {
	case PrimaryKey:
		pks = append(pks, key.ID(tx.meta.Name, k))
	case UniqueIndex:
		b, err := tx.db.client.Get(key.Unique(tx.meta.Name, fieldName, k))
		if err != nil {
			if errors.Cause(err) == client.ErrKeyNotFound {
				return 0, nil
			}
			return 0, err
		}
		pks = append(pks, string(b))
	case SecondaryIndex:
		x := key.Indexes(tx.meta.Name, fieldName, k)
		kvs, err := tx.db.client.Prefix(x)
		if err != nil {
			if errors.Cause(err) == client.ErrKeyNotFound {
				return 0, nil
			}
			return 0, err
		}
		for _, kv := range kvs {
			pks = append(pks, string(kv.Value))
		}
	default:
		return 0, errors.Wrapf(ErrNotIndexed, "cannot delete %#v", fieldName)
	}

	ops := make([]clientv3.Op, 0)
	for _, pk := range pks {
		keys, err := tx.getIndexesByPrimaryKey(pk)
		if err != nil {
			return 0, err
		}
		if len(keys) > 0 {
			n++
		}
		ops = append(ops, deleteOps(keys)...)
	}
	if _, err := tx.batchOps(ops...); err != nil {
		return 0, err
	}
	return n, nil
}

func (tx *Tx) DeleteAll() error {
	kvs, err := tx.db.client.Prefix(key.Table(tx.meta.Name))
	if err != nil {
		return err
	}
	ops := make([]clientv3.Op, 0)
	for _, kv := range kvs {
		if strings.Contains(string(kv.Key), key.TableDef(tx.meta.Name)) {
			continue
		}
		ops = append(ops, clientv3.OpDelete(string(kv.Key)))
	}
	_, err = tx.batchOps(ops...)
	return err
}

func (tx *Tx) Drop() error {
	v, err := tx.db.client.Get(key.TableDef(tx.meta.Name))
	if err != nil {
		if errors.Cause(err) != client.ErrKeyNotFound {
			return errors.Wrap(ErrTableNotFound, tx.meta.Name)
		}
		return err
	}
	var m *ModelDef
	if err := tx.c.Decode(v, &m); err != nil {
		return err
	}
	if err := tx.validateModel(m); err != nil {
		return err
	}
	resp, err := tx.db.client.Delete(context.TODO(), key.Table(tx.meta.Name), clientv3.WithPrefix())
	log.Debugf("dropped table %s, %d rows deleted", tx.meta.Name, resp.Deleted)
	return err
}
