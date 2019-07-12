package q

import (
	"errors"
	"reflect"
)

type Matcher interface {
	Match(v reflect.Value) (bool, error)
}

type fieldEq struct {
	name string
	v    interface{}
}

func (f *fieldEq) Match(other reflect.Value) (bool, error) {
	if other.Kind() == reflect.Ptr {
		other = other.Elem()
	}
	field := other.FieldByName(f.name)
	if !field.IsValid() {
		return false, errors.New("invalid field")
	}
	return reflect.DeepEqual(f.v, field.Interface()), nil
}

func Eq(name string, v interface{}) Matcher {
	return &fieldEq{name, v}
}

type and struct {
	matchers []Matcher
}

func (a *and) Match(other reflect.Value) (bool, error) {
	for _, m := range a.matchers {
		ok, err := m.Match(other)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func And(matchers ...Matcher) Matcher {
	return &and{matchers}
}

type or struct {
	matchers []Matcher
}

func (o *or) Match(other reflect.Value) (bool, error) {
	for _, m := range o.matchers {
		ok, err := m.Match(other)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func Or(matchers ...Matcher) Matcher {
	return &or{matchers}
}

type not struct {
	matchers []Matcher
}

func (n *not) Match(other reflect.Value) (bool, error) {
	for _, m := range n.matchers {
		ok, err := m.Match(other)
		if err != nil {
			return false, err
		}
		if ok {
			return false, nil
		}
	}
	return true, nil
}

func Not(matchers ...Matcher) Matcher {
	return &not{matchers}
}
