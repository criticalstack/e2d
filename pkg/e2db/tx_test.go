package e2db

import (
	"testing"

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
