package e2db

import (
	"reflect"
	"strings"

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
	return f.hasTag("index", "unique")
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

type ModelDef struct {
	Name   string
	Fields map[string]*FieldDef
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
		Fields: make(map[string]*FieldDef),
	}
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
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
		m.Fields[ft.Name] = &FieldDef{
			Name: ft.Name,
			Tags: tags,
		}
	}
	return m
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
