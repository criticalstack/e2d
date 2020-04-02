package e2db

import (
	"reflect"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/e2db/key"
	"github.com/criticalstack/e2d/pkg/e2db/q"
	"github.com/pkg/errors"
)

var _ Query = (*Table)(nil)

// TODO(chris): could probably do something cool like automatic schema
// migrations
type Table struct {
	db   *DB
	c    Codec
	tc   Codec
	meta *ModelDef
}

func (t *Table) validateModel(remote *ModelDef) error {
	if t.meta.Name != remote.Name {
		return errors.Errorf("type name mismatch, expected %#v, received %#v", remote.Name, t.meta.Name)
	}
	return nil
}

func (t *Table) validateSchema(typ reflect.Type) error {
	if typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	return t.validateModel(NewModelDef(typ))
}

func (t *Table) tableMustExist() error {
	v, err := t.db.client.Get(key.TableDef(t.meta.Name))
	if err != nil && errors.Cause(err) != client.ErrKeyNotFound {
		return err
	}
	if errors.Cause(err) == client.ErrKeyNotFound {
		data, err := t.tc.Encode(t.meta)
		if err != nil {
			return err
		}
		if err := t.db.client.Set(key.TableDef(t.meta.Name), string(data)); err != nil {
			return err
		}
		return nil
	}

	var m *ModelDef
	if err := t.tc.Decode(v, &m); err != nil {
		return err
	}
	return t.validateModel(m)
}

func (t *Table) All(to interface{}) error {
	return newQuery(t).All(to)
}

func (t *Table) Count(fieldName string, data interface{}) (int64, error) {
	return newQuery(t).Count(fieldName, data)
}

func (t *Table) Find(fieldName string, data interface{}, to interface{}) error {
	return newQuery(t).Find(fieldName, data, to)
}

func (t *Table) OrderBy(field string) Query {
	q := newQuery(t)
	q.sort = field
	return q
}

func (t *Table) Reverse() Query {
	q := newQuery(t)
	q.reverse = true
	return q
}

func (t *Table) Filter(matchers ...q.Matcher) Query {
	q := newQuery(t, matchers...)
	return q
}

func (t *Table) Limit(i int) Query {
	q := newQuery(t)
	q.limit = i
	return q
}

func (t *Table) Skip(i int) Query {
	q := newQuery(t)
	q.skip = i
	return q
}

func (t *Table) Delete(fieldName string, data interface{}) (int64, error) {
	var n int64
	err := t.Tx(func(tx *Tx) (err error) {
		n, err = tx.Delete(fieldName, data)
		return err
	})
	return n, err
}

func (t *Table) DeleteAll() error {
	return t.Tx(func(tx *Tx) error {
		return tx.DeleteAll()
	})
}

func (t *Table) Drop() error {
	return t.Tx(func(tx *Tx) error {
		return tx.Drop()
	})
}

func (t *Table) Insert(iface interface{}) error {
	return t.Tx(func(tx *Tx) error {
		return tx.Insert(iface)
	})
}

func (t *Table) Update(iface interface{}) error {
	return t.Tx(func(tx *Tx) error {
		return tx.Update(iface)
	})
}
