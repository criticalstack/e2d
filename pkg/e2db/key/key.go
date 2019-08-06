package key

import (
	"path/filepath"
	"strings"
)

const (
	indexPrefix = "_index"
	tablePrefix = "_table"
)

func join(parts ...string) string {
	path := filepath.Join(parts...)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func Hidden(model string) string {
	return join(model, "_")
}

func ID(model, key string) string {
	return join(model, key)
}

func Table(model string) string {
	return join(model) + "/"
}

func TableDef(model string) string {
	return join(model, tablePrefix)
}

func TableLock(model string) string {
	return join(model, tablePrefix, "lock")
}

func Increment(model, field string) string {
	return join(model, tablePrefix, field, "last")
}

func Index(model, field, value, id string) string {
	return join(model, indexPrefix, field, value, id)
}

func Indexes(model, field, value string) string {
	return join(model, indexPrefix, field, value)
}

func Unique(model, field, value string) string {
	return join(model, indexPrefix, field, value)
}
