package e2db

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/criticalstack/e2d/pkg/e2db/key"
	"github.com/pkg/errors"
)

var (
	ErrNoPrimaryKey = errors.New("primary key not defined")
)

type Tag struct {
	Name, Value string
}

func (t *Tag) String() string {
	if t.Value == "" {
		return t.Name
	}
	return fmt.Sprintf("%s=%s", t.Name, t.Value)
}

type FieldDef struct {
	Name   string
	Kind   reflect.Kind
	Type   string
	Tags   []*Tag
	Fields []*FieldDef
}

func (f *FieldDef) String() string {
	var tags string
	if len(f.Tags) > 0 {
		tt := make([]string, 0)
		for _, t := range f.Tags {
			tt = append(tt, t.String())
		}
		tags = fmt.Sprintf(" `%s`", strings.Join(tt, ","))
	}
	t := f.Kind.String()
	if f.Kind == reflect.Struct {
		t = f.Type
	}
	return fmt.Sprintf("%s %s%s", f.Name, t, tags)
}

func (f *FieldDef) isIndex() bool {
	return f.isPrimaryKey() || f.hasTag("index", "unique")
}

func (f *FieldDef) getTag(name string) *Tag {
	for _, t := range f.Tags {
		if t.Name == name {
			return t
		}
	}
	return nil
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

func (f *FieldDef) indexType() IndexType {
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
	switch f.indexType() {
	case PrimaryKey:
		return key.ID(tableName, value), nil
	case SecondaryIndex, UniqueIndex:
		return key.Indexes(tableName, f.Name, value), nil
	default:
		return "", errors.Wrap(ErrNotIndexed, f.Name)
	}
}

func newFieldDefs(t reflect.Type) []*FieldDef {
	fields := make([]*FieldDef, 0)
	for i := 0; i < t.NumField(); i++ {
		ft := t.Field(i)
		if unicode.IsLower([]rune(ft.Name)[0]) {
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
		sort.Slice(tags, func(i, j int) bool {
			return tags[i].Name < tags[j].Name
		})
		fd := &FieldDef{
			Name: ft.Name,
			Kind: ft.Type.Kind(),
			Type: ft.Type.String(),
			Tags: tags,
		}
		if ft.Type.Kind() == reflect.Struct {
			fd.Fields = newFieldDefs(ft.Type)
		}
		fields = append(fields, fd)
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Name < fields[j].Name
	})
	return fields
}

type ModelDef struct {
	Name     string
	Fields   []*FieldDef
	CheckSum string
	Version  string

	t reflect.Type
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
		Fields: newFieldDefs(t),
		t:      t,
	}
	if !m.hasPrimaryKey() {
		panic("must specify a primary key")
	}
	pk := m.getPrimaryKey()
	vt := pk.getTag("v")
	if vt == nil {
		vt = &Tag{Name: "v", Value: "0"}
		pk.Tags = append(pk.Tags, vt)
	}
	m.Version = vt.Value
	m.CheckSum = SchemaCheckSum(m)
	return m
}

func (m *ModelDef) New() *reflect.Value {
	if m.t == nil {
		return nil
	}
	v := reflect.New(m.t)
	return &v
}

func (m *ModelDef) getPrimaryKey() *FieldDef {
	for _, f := range m.Fields {
		if f.isPrimaryKey() {
			return f
		}
	}
	return nil
}

func (m *ModelDef) hasPrimaryKey() bool {
	return m.getPrimaryKey() != nil
}

func (m *ModelDef) getFieldByName(name string) (*FieldDef, bool) {
	for _, f := range m.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

func (m *ModelDef) String() string {
	return m.t.String()
}

type Field struct {
	*FieldDef

	v reflect.Value
}

func (f *Field) isZero() bool {
	return f.v.IsValid() && reflect.DeepEqual(f.v.Interface(), reflect.Zero(f.v.Type()).Interface())
}

type ModelItem struct {
	*ModelDef
	Fields []*Field
}

func NewModelItem(v reflect.Value) *ModelItem {
	v = reflect.Indirect(v)
	if v.Type().NumField() == 0 {
		panic("must have at least 1 struct field")
	}
	m := &ModelItem{
		ModelDef: NewModelDef(v.Type()),
		Fields:   make([]*Field, 0),
	}
	for _, f := range m.ModelDef.Fields {
		m.Fields = append(m.Fields, &Field{
			FieldDef: f,
			v:        v.FieldByName(f.Name),
		})
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

func schemaCheckSumFieldDef(f *FieldDef) string {
	var sb strings.Builder
	sb.WriteString(f.String())
	for _, f := range f.Fields {
		switch f.Kind {
		case reflect.Struct:
			sb.WriteString(schemaCheckSumFieldDef(f))
		default:
			sb.WriteString(f.String())
		}
	}
	return sb.String()
}

func SchemaCheckSum(m *ModelDef) string {
	var b bytes.Buffer
	for _, f := range m.Fields {
		b.WriteString(schemaCheckSumFieldDef(f))
	}
	h := sha1.Sum(b.Bytes())
	name := hex.EncodeToString(h[:])
	if len(name) > 5 {
		name = name[:5]
	}
	return strings.ToLower(name)
}

func printFieldDef(f *FieldDef) {
	fmt.Println(f)
	for _, f := range f.Fields {
		switch f.Kind {
		case reflect.Struct:
			printFieldDef(f)
		default:
			fmt.Println(f)
		}
	}
}

func PrintModelDef(m *ModelDef) {
	fmt.Println(m)
	for _, f := range m.Fields {
		switch f.Kind {
		case reflect.Struct:
			printFieldDef(f)
		default:
			fmt.Println(f)
		}
	}
}
