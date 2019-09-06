package e2db

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/criticalstack/e2d/pkg/e2db/key"
	"github.com/pkg/errors"
)

var (
	ErrNoPrimaryKey = errors.New("primary key not defined")
)

type Tag struct {
	Name, Value string
}

type FieldDef struct {
	Name string
	Tags []*Tag
}

func (f *FieldDef) isIndex() bool {
	return f.isPrimaryKey() || f.hasTag("index", "unique")
}

func (f *FieldDef) hasTag(tags ...string) bool {
	for _, t := range f.Tags {
		for _, tag := range tags {
			if t.Name == tag {
				return true
			}
		}
	}
	return false
}

func (f *FieldDef) isPrimaryKey() bool {
	for _, t := range f.Tags {
		switch t.Name {
		case "increment", "id":
			return true
		}
	}
	return false
}

type IndexType int

const (
	NoIndex IndexType = iota
	PrimaryKey
	SecondaryIndex
	UniqueIndex
)

func (f *FieldDef) Type() IndexType {
	switch {
	case f.hasTag("increment", "id"):
		return PrimaryKey
	case f.hasTag("index"):
		return SecondaryIndex
	case f.hasTag("unique"):
		return UniqueIndex
	default:
		return NoIndex
	}
}

func (f *FieldDef) indexKey(tableName string, value string) (string, error) {
	switch f.Type() {
	case PrimaryKey:
		return key.ID(tableName, value), nil
	case SecondaryIndex, UniqueIndex:
		return key.Indexes(tableName, f.Name, value), nil
	default:
		return "", errors.Wrap(ErrNotIndexed, f.Name)
	}
}

type ModelDef struct {
	Name   string
	Fields map[string]*FieldDef

	t reflect.Type
}

func readFields(t reflect.Type) map[string]*FieldDef {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.NumField() == 0 {
		panic("must have at least 1 struct field")
	}
	fields := make(map[string]*FieldDef)
	anon := make([]reflect.StructField, 0)
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		if ft.Anonymous && ft.Type.Kind() != reflect.Interface {
			anon = append(anon, ft)
			continue
		}
		tags := make([]*Tag, 0)
		if tagValue, ok := ft.Tag.Lookup("e2db"); ok {
			for _, t := range strings.Split(tagValue, ",") {
				parts := strings.SplitN(t, "=", 2)
				if len(parts) == 2 {
					tags = append(tags, &Tag{parts[0], parts[1]})
				} else {
					tags = append(tags, &Tag{Name: t})
				}
			}
		}
		fields[ft.Name] = &FieldDef{
			Name: ft.Name,
			Tags: tags,
		}
	}

	// Process anonymous/embedded struct fields in a second pass to allow parent
	// fields to supersede the embedded struct fields.
	for _, ft := range anon {
		for n, f := range readFields(ft.Type) {
			if _, ok := fields[n]; ok {
				// Field is superseded in parent struct, ignore.
				continue
			}
			if _, ok := t.FieldByName(n); !ok {
				panic(fmt.Sprintf("struct field in type %v is ambiguous: %q", t, n))
			}
			fields[n] = f
		}
	}
	return fields
}

func NewModelDef(t reflect.Type) *ModelDef {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.NumField() == 0 {
		panic("must have at least 1 struct field")
	}
	m := &ModelDef{
		Name:   t.Name(),
		Fields: readFields(t),
		t:      t,
	}
	return m
}

func (m *ModelDef) New() *reflect.Value {
	if m.t == nil {
		return nil
	}
	v := reflect.New(m.t)
	return &v
}

type Field struct {
	*FieldDef
	value reflect.Value
}

func (f *Field) isZero() bool {
	return f.value.IsValid() && reflect.DeepEqual(f.value.Interface(), reflect.Zero(f.value.Type()).Interface())
}

type ModelItem struct {
	*ModelDef
	Fields map[string]*Field
}

func NewModelItem(v reflect.Value) *ModelItem {
	v = reflect.Indirect(v)
	if v.Type().NumField() == 0 {
		panic("must have at least 1 struct field")
	}
	m := &ModelItem{
		ModelDef: NewModelDef(v.Type()),
		Fields:   make(map[string]*Field),
	}
	for name, f := range m.ModelDef.Fields {
		m.Fields[name] = &Field{
			FieldDef: f,
			value:    v.FieldByName(name),
		}
	}
	return m
}

func (m *ModelItem) getPrimaryKey() (*Field, error) {
	for _, f := range m.Fields {
		if f.isPrimaryKey() {
			return f, nil
		}
	}
	return nil, ErrNoPrimaryKey
}
