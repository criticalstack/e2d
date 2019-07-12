package q_test

import (
	"reflect"
	"testing"

	"github.com/criticalstack/e2d/pkg/e2db/q"
	"github.com/google/go-cmp/cmp"
)

type Role struct {
	ID             int    `e2db:"increment"`
	Name           string `e2db:"unique"`
	Description    string `e2db:"index,required"`
	ResourceQuota  string
	LimitRange     string
	SuperAdminOnly bool
}

var roles = []*Role{
	{ID: 1, Name: "user", Description: "user"},
	{ID: 2, Name: "admin", Description: "administrator"},
	{ID: 3, Name: "superadmin", Description: "administrator"},
	{ID: 4, Name: "smoot", Description: "administrator"},
}

func TestMatcher(t *testing.T) {
	cases := []struct {
		name     string
		m        q.Matcher
		expected []*Role
	}{
		{
			name: "AndEq",
			m:    q.And(q.Eq("Name", "smoot"), q.Eq("SuperAdminOnly", false)),
			expected: []*Role{
				{ID: 4, Name: "smoot", Description: "administrator"},
			},
		},
		{
			name: "OrEq",
			m:    q.Or(q.Eq("Name", "smoot"), q.Eq("Name", "superadmin")),
			expected: []*Role{
				{ID: 3, Name: "superadmin", Description: "administrator"},
				{ID: 4, Name: "smoot", Description: "administrator"},
			},
		},
		{
			name: "Eq",
			m:    q.Eq("Name", "smoot"),
			expected: []*Role{
				{ID: 4, Name: "smoot", Description: "administrator"},
			},
		},
		{
			name: "NotEq",
			m:    q.Not(q.Eq("Name", "smoot")),
			expected: []*Role{
				{ID: 1, Name: "user", Description: "user"},
				{ID: 2, Name: "admin", Description: "administrator"},
				{ID: 3, Name: "superadmin", Description: "administrator"},
			},
		},
	}
	for _, tc := range cases {
		matches := make([]*Role, 0)
		for _, role := range roles {
			ok, err := tc.m.Match(reflect.ValueOf(role))
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				matches = append(matches, role)
			}
		}
		if diff := cmp.Diff(tc.expected, matches); diff != "" {
			t.Errorf("%s: after Match differs: (-want +got)\n%s", tc.name, diff)
		}
	}
}
