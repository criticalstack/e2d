package e2db

import (
	"testing"

	"github.com/criticalstack/e2d/pkg/e2db/q"
	"github.com/google/go-cmp/cmp"
)

func TestFindManySort(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.OrderBy("ID").Find("Description", "administrator", &r)
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
	err = roles.OrderBy("ID").Reverse().Find("Description", "administrator", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected = []*Role{
		{ID: 4, Name: "smoot", Description: "administrator"},
		{ID: 4, Name: "smoot", Description: "administrator"},
		{ID: 3, Name: "superadmin", Description: "administrator"},
		{ID: 3, Name: "superadmin", Description: "administrator"},
		{ID: 2, Name: "admin", Description: "administrator"},
		{ID: 2, Name: "admin", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindManyFilter(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.Filter(q.Eq("Name", "smoot")).Find("Description", "administrator", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*Role{
		{ID: 4, Name: "smoot", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindManyFilterAnd(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.Filter(q.And(q.Eq("Name", "smoot"), q.Eq("SuperAdminOnly", false))).Find("Description", "administrator", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*Role{
		{ID: 4, Name: "smoot", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindManyFilterOr(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.Filter(q.Or(q.Eq("Name", "smoot"), q.Eq("Name", "superadmin"))).Find("Description", "administrator", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*Role{
		{ID: 3, Name: "superadmin", Description: "administrator"},
		{ID: 4, Name: "smoot", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}

func TestFindManyFilterNot(t *testing.T) {
	resetTable(t)
	roles := db.Table(&Role{})
	var r []*Role
	err := roles.Filter(q.Not(q.Eq("Name", "smoot"))).Find("Description", "administrator", &r)
	if err != nil {
		t.Fatal(err)
	}
	expected := []*Role{
		{ID: 2, Name: "admin", Description: "administrator"},
		{ID: 3, Name: "superadmin", Description: "administrator"},
	}
	if diff := cmp.Diff(expected, r); diff != "" {
		t.Errorf("e2db: after Find differs: (-want +got)\n%s", diff)
	}
}
