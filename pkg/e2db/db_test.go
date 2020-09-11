package e2db_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"go.uber.org/zap/zapcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/criticalstack/e2d/e2e"
	configv1alpha1 "github.com/criticalstack/e2d/pkg/config/v1alpha1"
	"github.com/criticalstack/e2d/pkg/e2db"
	"github.com/criticalstack/e2d/pkg/log"
	"github.com/criticalstack/e2d/pkg/manager"
)

var db *e2db.DB

func init() {
	log.SetLevel(zapcore.DebugLevel)

	if err := os.RemoveAll("testdata"); err != nil {
		log.Fatal(err)
	}

	m, err := manager.New(&configv1alpha1.Configuration{
		OverrideName:        "node1",
		ClientAddr:          e2e.ParseAddr(":2479"),
		PeerAddr:            e2e.ParseAddr(":2480"),
		GossipAddr:          e2e.ParseAddr(":7980"),
		DataDir:             filepath.Join("testdata", "node1"),
		RequiredClusterSize: 1,
		HealthCheckInterval: metav1.Duration{Duration: 1 * time.Second},
		HealthCheckTimeout:  metav1.Duration{Duration: 5 * time.Second},
	})
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := m.Run(); err != nil {
			log.Fatal(err)
		}
	}()
	db, err = e2db.New(context.Background(), &e2db.Config{
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
	if err := roles.Drop(); err != nil && errors.Cause(err) != e2db.ErrTableNotFound {
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
	if errors.Cause(err) != e2db.ErrNotIndexed {
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
	if errors.Cause(err) != e2db.ErrFieldRequired {
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
			err:   e2db.ErrNotIndexed,
		},
		{
			name:  "masked field real value",
			field: "Masked",
			value: "real value",
			err:   e2db.ErrNotIndexed,
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

func TestEncryptedTable(t *testing.T) {
	db, err := e2db.New(context.Background(), &e2db.Config{
		ClientAddr: ":2479",
		Namespace:  "encrypted",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	roles := db.Table(&Role{})
	rolesEncrypted := db.Table(&Role{}, e2db.WithEncryption([]byte("secret")))
	if err := rolesEncrypted.Insert(&Role{Name: "user", Description: "user"}); err != nil {
		t.Fatal(err)
	}
	var r Role
	if err := roles.Find("ID", 1, &r); err == nil {
		t.Fatalf("expected err decrypting role: %v", r)
	}
	if err := rolesEncrypted.Find("Name", "user", &r); err != nil {
		time.Sleep(1 * time.Second)
		t.Fatal(err)
	}
	if err := rolesEncrypted.Drop(); err != nil && errors.Cause(err) != e2db.ErrTableNotFound {
		t.Fatal(err)
	}
	expected := &Role{ID: 1, Name: "user", Description: "user"}
	if diff := cmp.Diff(expected, &r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
	time.Sleep(1 * time.Second)
}

type Cert struct {
	Path        string `e2db:"id"`
	Description string `e2db:"index"`
	Data        []byte `e2db:"encrypted"`
}

func TestEncryptedField(t *testing.T) {
	db, err := e2db.New(context.Background(), &e2db.Config{
		ClientAddr: ":2479",
		Namespace:  "criticalstack",
		SecretKey:  []byte("secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	certs := db.Table(&Cert{})
	cert := &Cert{Path: "ca.crt", Description: "cluster cert", Data: []byte("secret data")}
	if err := certs.Insert(cert); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(cert.Data, []byte("ENCRYPTED:")) {
		t.Fatalf("field was not encrypted: %+v", cert)
	}
	var c Cert
	if err := certs.Find("Description", "cluster cert", &c); err != nil {
		time.Sleep(1 * time.Second)
		t.Fatal(err)
	}
	expected := &Cert{Path: "ca.crt", Description: "cluster cert", Data: []byte("secret data")}
	if diff := cmp.Diff(expected, &c); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
	var cc []*Cert
	if err := certs.All(&cc); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(expected, cc[0]); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
	c.Description = "updated cert"
	c.Data = []byte("updated secret")
	if err := certs.Update(c); err != nil {
		t.Fatal(err)
	}
	c = Cert{}
	err = certs.Find("Description", "updated cert", &c)
	if err != nil {
		t.Fatal(err)
	}
	expected = &Cert{Path: "ca.crt", Description: "updated cert", Data: []byte("updated secret")}
	if diff := cmp.Diff(expected, &c); diff != "" {
		t.Errorf("e2db: after Update differs: (-want +got)\n%s", diff)
	}
	if err := certs.Drop(); err != nil && errors.Cause(err) != e2db.ErrTableNotFound {
		t.Fatal(err)
	}
}
