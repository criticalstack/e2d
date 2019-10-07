package e2db

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"go.uber.org/zap/zapcore"
)

var db *DB

func init() {
	log.SetLevel(zapcore.DebugLevel)

	if err := os.RemoveAll("testdata"); err != nil {
		log.Fatal(err)
	}

	m, err := manager.New(&manager.Config{
		Name:                "node1",
		ClientAddr:          ":2479",
		PeerAddr:            ":2480",
		GossipAddr:          ":7980",
		Dir:                 filepath.Join("testdata", "node1"),
		RequiredClusterSize: 1,
		HealthCheckInterval: 1 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		EtcdLogLevel:        zapcore.WarnLevel,
	})
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := m.Run(); err != nil {
			log.Fatal(err)
		}
	}()
	db, err = New(&Config{
		ClientAddr: ":2479",
		Namespace:  "criticalstack",
	})
	if err != nil {
		log.Fatal(err)
	}
}

type Role struct {
	ID             int    `e2db:"increment"`
	Name           string `e2db:"unique"`
	Description    string `e2db:"index,required"`
	ResourceQuota  string
	LimitRange     string
	SuperAdminOnly bool
	NotIndexed     string
}

var newRoles = []*Role{
	{Name: "user", Description: "user"},
	{Name: "admin", Description: "administrator"},
	{Name: "superadmin", Description: "administrator"},
	{Name: "smoot", Description: "administrator"},
}

func resetTable(t *testing.T) {
	roles := db.Table(&Role{})
	if err := roles.Drop(); err != nil && errors.Cause(err) != ErrTableNotFound {
		t.Fatal(err)
	}
	for _, r := range newRoles {
		err := roles.Insert(r)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindOnePrimaryIndex(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r Role
	err := roles.Find("ID", 1, &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := &Role{ID: 1, Name: "user", Description: "user"}
	if diff := cmp.Diff(expected, &r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindOneUnique(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r Role
	err := roles.Find("Name", "superadmin", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := &Role{ID: 3, Name: "superadmin", Description: "administrator"}
	if diff := cmp.Diff(expected, &r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindManyIndex(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.Find("Description", "administrator", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*Role{
		{ID: 2, Name: "admin", Description: "administrator"},
		{ID: 3, Name: "superadmin", Description: "administrator"},
		{ID: 4, Name: "smoot", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindManyNoIndex(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.Find("NotIndexed", "value", &r)
	if errors.Cause(err) != ErrNotIndexed {
		t.Fatal("expect 'field is not indexed' error")
	}
}

func TestFindAll(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.All(&r)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*Role{
		{ID: 1, Name: "user", Description: "user"},
		{ID: 2, Name: "admin", Description: "administrator"},
		{ID: 3, Name: "superadmin", Description: "administrator"},
		{ID: 4, Name: "smoot", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestInsertRequired(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	err := roles.Insert(&Role{Name: "invalid"})
	if errors.Cause(err) != ErrFieldRequired {
		t.Fatal("expected ErrFieldRequired")
	}
}

func TestCount(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	n, err := roles.Count("Description", "administrator")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("expected count 3, received %d", n)
	}
}

func TestCountPrimaryIndex(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	n, err := roles.Count("ID", 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected count 1, received %d", n)
	}

	n, err = roles.Count("ID", 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected count 0, received %d", n)
	}
}

func TestUpdate(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r Role
	err := roles.Find("Description", "user", &r)
	if err != nil {
		t.Fatal(err)
	}
	r.Description = "updated user"
	if err := roles.Update(r); err != nil {
		t.Fatal(err)
	}
	r = Role{}
	err = roles.Find("Description", "updated user", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := &Role{ID: 1, Name: "user", Description: "updated user"}
	if diff := cmp.Diff(expected, &r); diff != "" {
		t.Errorf("e2db: after Update differs: (-want +got)\n%s", diff)
	}
}

func TestNestedFieldQuery(t *testing.T) {
	type Nested struct {
		Name        string `e2db:"unique"`
		Other       int
		MaskedIndex string
		Masked      string `e2db:"index"`
	}
	type Model struct {
		ID int `e2db:"increment"`
		Nested
		Email       string `e2db:"index"`
		MaskedIndex string `e2db:"index"`
		Masked      string
	}

	table := db.Table(&Model{})

	rows := []*Model{
		{
			Nested: Nested{
				Name:  "Steve",
				Other: 42,
			},
			Email: "steve@mail.org",
		},
		{
			Nested: Nested{
				Name:  "Smoot",
				Other: 666,
			},
			Email: "smoot@wellington.me",
		},
		{
			Nested: Nested{
				Name:        "Jim",
				Masked:      "test",
				MaskedIndex: "indexed",
			},
			Email:       "mail@themask.movie",
			Masked:      "real value",
			MaskedIndex: "real indexed value",
		},
	}

	for _, row := range rows {
		if err := table.Insert(row); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name     string
		field    string
		value    interface{}
		expected int64
		err      error
	}{
		{
			name:     "count nested field",
			field:    "Name",
			value:    "Smoot",
			expected: 1,
		},
		{
			name:  "masked field",
			field: "Masked",
			value: "test",
			err:   ErrNotIndexed,
		},
		{
			name:  "masked field real value",
			field: "Masked",
			value: "real value",
			err:   ErrNotIndexed,
		},
		{
			name:     "masked index",
			field:    "MaskedIndex",
			value:    "indexed",
			expected: 0,
		},
		{
			name:     "masked index real value",
			field:    "MaskedIndex",
			value:    "real indexed value",
			expected: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, err := table.Count(c.field, c.value)
			if errors.Cause(c.err) != errors.Cause(err) {
				t.Fatalf("expected error %v, got %v", c.err, err)
			}
			if c.expected != n {
				t.Fatalf("expected %d result(s), got %d", c.expected, n)
			}
		})
	}
}
