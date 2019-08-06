package e2db

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

func TestTxInsert(t *testing.T) {
	roles := db.Table(&Role{})
	if err := roles.Drop(); err != nil && errors.Cause(err) != ErrTableNotFound {
		t.Fatal(err)
	}
	err := roles.Tx(func(tx *Tx) error {
		for _, r := range newRoles {
			err := tx.Insert(r)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTxDelete(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		value  interface{}
		filter func(*Role) bool
		n      int64
		err    error
	}{
		{
			name:   "delete absent key",
			field:  "ID",
			value:  20,
			filter: func(r *Role) bool { return r.ID == 20 },
		},
		{
			name:   "delete primary key",
			field:  "ID",
			value:  3,
			n:      1,
			filter: func(r *Role) bool { return r.ID == 3 },
		},
		{
			name:   "delete secondary index",
			field:  "Description",
			value:  "administrator",
			n:      3,
			filter: func(r *Role) bool { return r.Description == "administrator" },
		},
		{
			name:   "delete unique index",
			field:  "Name",
			value:  "smoot",
			n:      1,
			filter: func(r *Role) bool { return r.Name == "smoot" },
		},
		{
			name:   "delete secondary index not found",
			field:  "Description",
			value:  "dinosaur",
			filter: func(r *Role) bool { return r.Description == "dinosaur" },
		},
		{
			name:   "delete unique index not found",
			field:  "Name",
			value:  "smootest",
			filter: func(r *Role) bool { return r.Name == "smootest" },
		},
		{
			name:  "delete non-index",
			field: "NotIndexed",
			value: "something",
			err:   ErrNotIndexed,
		},
		// TODO(ktravis): make field/value a map to test multiple deletions in sequence
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetTable(t)
			roles := db.Table(&Role{})
			n, err := roles.Delete(c.field, c.value)
			if errors.Cause(err) != c.err {
				t.Fatalf("expected error %v, got %v", c.err, err)
			}
			if c.err == nil {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				return
			}
			if n != c.n {
				t.Fatalf("expected delete count %d, got %d", c.n, n)
			}

			expected := append([]*Role{}, newRoles...)
			for i := 0; i < len(expected); i++ {
				if c.filter(expected[i]) {
					expected = append(expected[:i], expected[i+1:]...)
					i--
				}
			}
			var result []*Role
			if err := roles.All(&result); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(expected, result); diff != "" {
				t.Fatal("results did not match expected", diff)
			}
		})
	}
}
