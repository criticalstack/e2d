package e2db_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/criticalstack/e2d/pkg/e2db"
)

type ModelEnum int

const (
	Invalid ModelEnum = iota
	EnumVal1
	EnumVal2
)

type NestedStruct struct {
	Name  string
	Count int
}

type Model1 struct {
	Name      string `e2db:"unique,required"`
	ID        int    `e2db:"id"`
	CreatedAt time.Time
	Stats     NestedStruct
	Enum      ModelEnum
}

type Model2 struct {
	ID        int    `e2db:"id"`
	Name      string `e2db:"unique,required"`
	CreatedAt time.Time
	Stats     NestedStruct
	Enum      ModelEnum
}

func TestSchemaCheckSumArbitraryOrder(t *testing.T) {
	m := e2db.NewModelDef(reflect.TypeOf(&Model1{}))
	fmt.Println(m.String())
	fmt.Println(m.CheckSum)
	m = e2db.NewModelDef(reflect.TypeOf(&Model2{}))
	fmt.Println(m.String())
	fmt.Println(m.CheckSum)
}
