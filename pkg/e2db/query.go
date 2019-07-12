package e2db

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/criticalstack/e2d/pkg/client"
	"github.com/criticalstack/e2d/pkg/e2db/key"
	"github.com/criticalstack/e2d/pkg/e2db/q"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	ErrNotIndexed   = errors.New("field is not indexed")
	ErrInvalidField = errors.New("invalid field name")
	ErrNoRows       = errors.New("no rows found")
)

type Query interface {
	OrderBy(string) Query
	Reverse() Query
	Filter(...q.Matcher) Query
	Limit(int) Query
	Skip(int) Query
	All(interface{}) error
	Count(string, interface{}) (int64, error)
	Find(string, interface{}, interface{}) error
}

type query struct {
	t         *Table
	matchers  []q.Matcher
	countOnly bool
	limit     int
	skip      int
	sort      string
	reverse   bool
}

func newQuery(t *Table, matchers ...q.Matcher) *query {
	q := &query{
		t:        t,
		matchers: matchers,
	}
	return q
}

func (q *query) OrderBy(field string) Query {
	q.sort = field
	return q
}

func (q *query) Reverse() Query {
	q.reverse = true
	return q
}

func (q *query) Filter(matchers ...q.Matcher) Query {
	q.matchers = matchers
	return q
}

// TODO(chris): doesn't work
func (q *query) Limit(i int) Query {
	q.limit = i
	return q
}

// TODO(chris): doesn't work
func (q *query) Skip(i int) Query {
	q.skip = i
	return q
}

func (q *query) findOneByPrimaryKey(key string, v reflect.Value) error {
	value, err := q.t.db.client.Get(key)
	if err != nil {
		if errors.Cause(err) == client.ErrKeyNotFound {
			return errors.Wrapf(ErrNoRows, "findOneByPrimaryKey: %#v", key)
		}
		return err
	}
	return q.t.c.Decode(value, v.Addr().Interface())
}

func (q *query) findOneByUniqueIndex(key string, v reflect.Value) error {
	pk, err := q.t.db.client.Get(key)
	if err != nil {
		if errors.Cause(err) == client.ErrKeyNotFound {
			return errors.Wrapf(ErrNoRows, "findOneByIndex: %#v", key)
		}
		return err
	}
	return q.findOneByPrimaryKey(string(pk), v)
}

func (q *query) findOneBySecondaryIndex(key string, v reflect.Value) error {
	kvs, err := q.t.db.client.Prefix(key)
	if err != nil {
		if errors.Cause(err) == client.ErrKeyNotFound {
			return errors.Wrapf(ErrNoRows, "findOneByIndex: %#v", key)
		}
		return err
	}
	return q.findOneByPrimaryKey(string(kvs[0].Value), v)
}

func (q *query) findManyByIndex(key string, v reflect.Value) error {
	kvs, err := q.t.db.client.Prefix(key)
	if err != nil {
		if errors.Cause(err) == client.ErrKeyNotFound {
			return errors.Wrapf(ErrNoRows, "findManyByIndex: %#v", key)
		}
		return err
	}
	for i, kv := range kvs {
		if q.limit != 0 && q.limit <= i {
			fmt.Println("reached limit")
			break
		}
		item := reflect.New(v.Type().Elem())
		if err := q.findOneByPrimaryKey(string(kv.Value), reflect.Indirect(item)); err != nil {
			return err
		}
		if len(q.matchers) == 0 {
			v.Set(reflect.Append(v, item.Elem()))
			continue
		}
		for _, m := range q.matchers {
			ok, err := m.Match(item.Elem())
			if err != nil {
				return err
			}
			if ok {
				v.Set(reflect.Append(v, item.Elem()))
			}
		}
	}
	if q.sort == "" {
		return nil
	}
	s, err := newSorter(v, q.sort, q.reverse)
	if err != nil {
		return err
	}
	sort.Sort(s)
	return nil
}

func (q *query) findAll(table string, v reflect.Value) error {
	kvs, err := q.t.db.client.Prefix(key.Table(table))
	if err != nil {
		if errors.Cause(err) == client.ErrKeyNotFound {
			return ErrNoRows
		}
		return err
	}
	for _, kv := range kvs {
		if strings.Contains(string(kv.Key), key.Hidden(table)) {
			continue
		}
		item := reflect.New(v.Type().Elem())
		if err := q.t.c.Decode(kv.Value, item.Interface()); err != nil {
			return err
		}
		if len(q.matchers) == 0 {
			v.Set(reflect.Append(v, item.Elem()))
			continue
		}
		for _, m := range q.matchers {
			ok, err := m.Match(item)
			if err != nil {
				return err
			}
			if ok {
				v.Set(reflect.Append(v, item.Elem()))
			}
		}
		v.Set(reflect.Append(v, item.Elem()))
	}
	if v.Len() == 0 {
		return ErrNoRows
	}
	if q.sort == "" {
		return nil
	}
	s, err := newSorter(v, q.sort, q.reverse)
	if err != nil {
		return err
	}
	sort.Sort(s)
	return nil
}

func (q *query) All(to interface{}) error {
	if err := q.t.tableMustExist(); err != nil {
		return err
	}
	v := reflect.Indirect(reflect.ValueOf(to))
	if v.Type().Kind() != reflect.Slice {
		return errors.New("results value must be a slice")
	}
	vt := v.Type().Elem()
	if vt.Kind() == reflect.Ptr {
		vt = vt.Elem()
	}
	if err := q.t.validateModel(NewModelDef(vt)); err != nil {
		return err
	}
	return q.findAll(q.t.meta.Name, v)
}

func (q *query) Count(fieldName string, data interface{}) (int64, error) {
	if err := q.t.tableMustExist(); err != nil {
		return 0, err
	}
	f, ok := q.t.meta.Fields[fieldName]
	if !ok {
		return 0, errors.Wrap(ErrInvalidField, fieldName)
	}
	if !f.isIndex() {
		return 0, errors.Wrap(ErrNotIndexed, fieldName)
	}
	return q.t.db.client.Count(key.Indexes(q.t.meta.Name, f.Name, toString(data)))
}

func (q *query) Find(fieldName string, data interface{}, to interface{}) error {
	st := time.Now()
	defer func() {
		log.Debug("query.Find",
			zap.String("key", fmt.Sprintf("%s/%v", q.t.meta.Name, fieldName)),
			zap.String("q", toString(data)),
			zap.Duration("elapsed", time.Now().Sub(st)),
		)
	}()
	if err := q.t.tableMustExist(); err != nil {
		return err
	}
	v := reflect.Indirect(reflect.ValueOf(to))
	if err := q.t.validateSchema(v.Type()); err != nil {
		return err
	}
	f, ok := q.t.meta.Fields[fieldName]
	if !ok {
		return errors.Errorf("invalid field name: %#v", fieldName)
	}
	if v.Type().Kind() == reflect.Slice {
		return q.findManyByIndex(key.Indexes(q.t.meta.Name, f.Name, toString(data)), v)
	}
	k := toString(data)
	switch f.Type() {
	case PrimaryKey:
		return q.findOneByPrimaryKey(key.ID(q.t.meta.Name, k), v)
	case UniqueIndex:
		return q.findOneByUniqueIndex(key.Indexes(q.t.meta.Name, f.Name, k), v)
	case SecondaryIndex:
		return q.findOneBySecondaryIndex(key.Indexes(q.t.meta.Name, f.Name, k), v)
	default:
		return errors.Errorf("field is not indexed: %#v", fieldName)
	}
}

func (q *query) MustFind(fieldName string, data interface{}, to interface{}) error {
	return nil
}
