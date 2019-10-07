//nolint:structcheck,unused
package e2db_test

import (
	"reflect"
	"testing"

	"github.com/criticalstack/e2d/pkg/e2db"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func field(name string, tags ...string) *e2db.FieldDef {
	f := &e2db.FieldDef{
		Name: name,
		Tags: []*e2db.Tag{},
	}
	for _, t := range tags {
		f.Tags = append(f.Tags, &e2db.Tag{Name: t})
	}
	return f
}

func fieldMap(ff ...*e2db.FieldDef) map[string]*e2db.FieldDef {
	m := make(map[string]*e2db.FieldDef)
	for _, f := range ff {
		m[f.Name] = f
	}
	return m
}

func TestNewModelDefUnboundedRecursion(t *testing.T) {
	type testModel struct {
		ID          string `e2db:"id"`
		Name        string `e2db:"unique"`
		Age         int
		favoriteNum int
		*testModel
	}

	expected := &e2db.ModelDef{
		Name: "testModel",
		Fields: fieldMap(
			field("Age"),
			field("ID", "id"),
			field("Name", "unique"),
			field("favoriteNum"),
		),
	}
	def := e2db.NewModelDef(reflect.TypeOf(new(testModel)))
	if diff := cmp.Diff(expected, def, cmpopts.IgnoreUnexported(e2db.ModelDef{})); diff != "" {
		t.Fatalf("ModelDef did not match expected: %v", diff)
	}
}

func TestNewModelDef(t *testing.T) {
	cases := []struct {
		name        string
		model       func() reflect.Type
		expected    *e2db.ModelDef
		expectPanic bool
	}{
		{
			name: "simple",
			model: func() reflect.Type {
				type testModel struct {
					ID          string `e2db:"id"`
					Name        string `e2db:"unique"`
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModel))
			},
			expected: &e2db.ModelDef{
				Name: "testModel",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					field("Name", "unique"),
					field("favoriteNum"),
				),
			},
		},
		{
			name: "embedded not struct",
			model: func() reflect.Type {
				type nest int
				type testModelNested struct {
					ID          string `e2db:"id"`
					Age         int
					favoriteNum int
					nest
				}
				return reflect.TypeOf(new(testModelNested))
			},
			expected: &e2db.ModelDef{
				Name: "testModelNested",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					field("favoriteNum"),
					field("nest"),
				),
			},
		},
		{
			name: "nested",
			model: func() reflect.Type {
				type nest struct {
					Name string `e2db:"unique"`
				}
				type testModelNested struct {
					ID string `e2db:"id"`
					nest
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModelNested))
			},
			expected: &e2db.ModelDef{
				Name: "testModelNested",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					field("Name", "unique"),
					field("favoriteNum"),
				),
			},
		},
		{
			name: "nested struct pointer",
			model: func() reflect.Type {
				type nest struct {
					Name string `e2db:"unique"`
				}
				type testModelNested struct {
					ID string `e2db:"id"`
					*nest
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModelNested))
			},
			expected: &e2db.ModelDef{
				Name: "testModelNested",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					field("Name", "unique"),
					field("favoriteNum"),
				),
			},
		},
		{
			name: "nested interface",
			model: func() reflect.Type {
				type nest interface {
					Name() string
				}
				type testModel struct {
					ID string `e2db:"id"`
					nest
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModel))
			},
			expected: &e2db.ModelDef{
				Name: "testModel",
				Fields: fieldMap(
					field("Age"),
					// embedded interface is treated as opaque value
					field("nest"),
					field("ID", "id"),
					field("favoriteNum"),
				),
			},
		},
		{
			name: "nesting preempted",
			model: func() reflect.Type {
				type nest struct {
					Name  string `e2db:"unique"`
					Other float64
				}
				type testModel struct {
					ID string `e2db:"id"`
					nest
					Name        string
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModel))
			},
			expected: &e2db.ModelDef{
				Name: "testModel",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					// no tag on this one, because the parent struct preempts the nest
					field("Name"),
					field("favoriteNum"),
					field("Other"),
				),
			},
		},
		{
			name: "ambiguous",
			model: func() reflect.Type {
				type nestA struct {
					Name string `e2db:"unique"`
				}
				type nestB struct {
					Name string `e2db:"index"`
				}
				type testModel struct {
					nestA
					nestB
					ID          string `e2db:"id"`
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModel))
			},
			expectPanic: true,
		},
		{
			name: "ambiguity resolved",
			model: func() reflect.Type {
				type nestA struct {
					Name string `e2db:"unique"`
				}
				type nestB struct {
					Name  string `e2db:"index"`
					Other float64
				}
				type testModel struct {
					nestA
					nestB
					ID          string `e2db:"id"`
					Name        string
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModel))
			},
			expected: &e2db.ModelDef{
				Name: "testModel",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					// ambiguity is resolved by parent override
					field("Name"),
					field("favoriteNum"),
					field("Other"),
				),
			},
		},
		{
			name: "recursive",
			model: func() reflect.Type {
				type nestInner struct {
					Name string `e2db:"unique"`
				}
				type nestOuter struct {
					nestInner
					Other float64
				}
				type testModel struct {
					nestOuter
					ID          string `e2db:"id"`
					Age         int
					favoriteNum int
				}
				return reflect.TypeOf(new(testModel))
			},
			expected: &e2db.ModelDef{
				Name: "testModel",
				Fields: fieldMap(
					field("Age"),
					field("ID", "id"),
					field("Name", "unique"),
					field("favoriteNum"),
					field("Other"),
				),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.expectPanic {
				defer func() {
					if x := recover(); x == nil {
						t.Fatal("expected NewModelDef to panic")
					}
				}()
			}
			def := e2db.NewModelDef(c.model())
			if diff := cmp.Diff(c.expected, def, cmpopts.IgnoreUnexported(e2db.ModelDef{})); diff != "" {
				t.Fatalf("ModelDef did not match expected: %v", diff)
			}
		})
	}
}
