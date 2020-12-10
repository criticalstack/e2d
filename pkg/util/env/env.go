package env

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// SetEnvs sets field values for the provided struct passed in based on
// environment variables. Fields are mapped to environment variables using the
// `env` struct tag. Non-tagged fields are skipped.
func SetEnvs(iface interface{}) error {
	v := reflect.Indirect(reflect.ValueOf(iface))
	if v.Kind() != reflect.Struct {
		return errors.Errorf("expected struct, received %v", v.Type())
	}
	for i := 0; i < v.Type().NumField(); i++ {
		fv := v.Field(i)
		tv, ok := v.Type().Field(i).Tag.Lookup("env")
		if !ok {
			continue
		}
		if v, ok := os.LookupEnv(strings.ToUpper(tv)); ok {
			if err := setValue(fv, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func setValue(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if isTimeDuration(v) {
			d, err := time.ParseDuration(s)
			if err != nil {
				return err
			}
			v.Set(reflect.ValueOf(d))
			return nil
		}
		i, err := strconv.ParseInt(s, 0, v.Type().Bits())
		if err != nil {
			return err
		}

		v.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := strconv.ParseUint(s, 0, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetUint(i)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		v.SetBool(b)
	default:
		return errors.Errorf("cannot set value for type: %v", v.Type())
	}
	return nil
}

func isTimeDuration(v reflect.Value) bool {
	return v.Kind() == reflect.Int64 && v.Type().PkgPath() == "time" && v.Type().Name() == "Duration"
}
