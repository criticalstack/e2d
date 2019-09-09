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

func typeIsEqual(t1, t2 reflect.Type) bool {
	if t1.Kind() == reflect.Ptr {
		t1 = t1.Elem()
	}
	if t2.Kind() == reflect.Ptr {
		t2 = t2.Elem()
	}
	return t1 == t2
}

// readStructFields constructs a map of FieldDefs from the provided struct
// type. The function only recurses into struct types when being embedded and
// will promote fields from the embedded type to the embedding type when there
// isn't a naming conflict.
func readStructFields(t reflect.Type) map[string]*FieldDef {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic("must provide struct type")
	}
	if t.NumField() == 0 {
		panic("must have at least 1 struct field")
	}
	fields := make(map[string]*FieldDef)
	embeddedFields := make([]reflect.StructField, 0)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if typeIsEqual(f.Type, t) {
			continue
		}
		if f.Anonymous && f.Type.Kind() != reflect.Interface {
			embeddedFields = append(embeddedFields, f)
			continue
		}
		tags := make([]*Tag, 0)
		if tagValue, ok := f.Tag.Lookup("e2db"); ok {
			for _, t := range strings.Split(tagValue, ",") {
				parts := strings.SplitN(t, "=", 2)
				if len(parts) == 2 {
					tags = append(tags, &Tag{parts[0], parts[1]})
				} else {
					tags = append(tags, &Tag{Name: t})
				}
			}
		}
		fields[f.Name] = &FieldDef{
			Name: f.Name,
			Tags: tags,
		}
	}

	// promote any embedded struct fields
	for _, f := range embeddedFields {
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Struct:
			for n, f := range readStructFields(ft) {
				if _, ok := fields[n]; ok {
					continue
				}
				if _, ok := t.FieldByName(n); !ok {
					panic(fmt.Sprintf("struct field in type %v is ambiguous: %q", t, n))
				}
				fields[n] = f
			}
		default:
			if _, ok := fields[f.Name]; ok {
				continue
			}
			fields[f.Name] = &FieldDef{
				Name: f.Name,
				Tags: make([]*Tag, 0),
			}
		}
	}
	return fields
}

type ModelDef struct {
	Name   string
	Fields map[string]*FieldDef

	t reflect.Type
}

func NewModelDef(t reflect.Type) *ModelDef {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic("must provide struct type")
	}
	if t.NumField() == 0 {
		panic("must have at least 1 struct field")
	}
	m := &ModelDef{
		Name:   t.Name(),
		Fields: readStructFields(t),
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
