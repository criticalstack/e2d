package e2db

import (
	"fmt"
	"strconv"

	"github.com/pkg/errors"
)

func toString(data interface{}) string {
	switch t := data.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	case error:
		return t.Error()
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(t)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int8:
		return strconv.FormatInt(int64(t), 10)
	case int16:
		return strconv.FormatInt(int64(t), 10)
	case int32:
		return strconv.Itoa(int(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case uint:
		return strconv.FormatInt(int64(t), 10)
	case uint8:
		return strconv.FormatInt(int64(t), 10)
	case uint16:
		return strconv.FormatInt(int64(t), 10)
	case uint32:
		return strconv.FormatInt(int64(t), 10)
	case uint64:
		return strconv.FormatInt(int64(t), 10)
	case []byte:
		return string(t)
	default:
		panic(errors.Errorf("unknown type: %T", data))
	}
}
