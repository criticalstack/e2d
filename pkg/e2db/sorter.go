package e2db

import (
	"reflect"

	"github.com/pkg/errors"
)

type sorter struct {
	v       reflect.Value
	fields  []reflect.Value
	reverse bool
}

func newSorter(v reflect.Value, name string, reverse bool) (*sorter, error) {
	if v.Kind() != reflect.Slice {
		return nil, errors.Errorf("expected slice, received %T", v.Interface())
	}
	s := &sorter{
		v:       v,
		fields:  make([]reflect.Value, v.Len()),
		reverse: reverse,
	}
	for i := range s.fields {
		s.fields[i] = reflect.Indirect(reflect.Indirect(v.Index(i)).FieldByName(name))
	}
	return s, nil
}

func (s *sorter) Len() int {
	return s.v.Len()
}

func (s *sorter) Less(i, j int) bool {
	a := toString(s.fields[i].Interface())
	b := toString(s.fields[j].Interface())
	if s.reverse {
		return a > b
	}
	return a < b
}

func (s *sorter) Swap(i, j int) {
	s.fields[i], s.fields[j] = s.fields[j], s.fields[i]
	reflect.Swapper(s.v.Interface())(i, j)
}
