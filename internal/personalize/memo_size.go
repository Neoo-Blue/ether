package personalize

import (
	"reflect"
	"time"
)

// memoSize accounts for retained slice capacities, strings, maps and pointed
// objects. Shared objects are deliberately charged more than once; the cache
// may decline a large screen but must never truncate its shelves.
func memoSize(v reflect.Value) int64 {
	if !v.IsValid() {
		return 0
	}
	if v.Type() == reflect.TypeFor[time.Time]() {
		return 0
	}
	switch v.Kind() {
	case reflect.String:
		return int64(v.Len())
	case reflect.Interface:
		if v.IsNil() {
			return 0
		}
		return memoSize(v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			return 0
		}
		return int64(v.Type().Elem().Size()) + memoSize(v.Elem())
	case reflect.Slice:
		if v.IsNil() {
			return 0
		}
		n := int64(v.Cap()) * int64(v.Type().Elem().Size())
		v = v.Slice(0, v.Cap())
		for i := 0; i < v.Len(); i++ {
			n += memoSize(v.Index(i))
			if n > memoMaxBytes {
				return n
			}
		}
		return n
	case reflect.Array:
		var n int64
		for i := 0; i < v.Len(); i++ {
			n += memoSize(v.Index(i))
		}
		return n
	case reflect.Struct:
		var n int64
		for i := 0; i < v.NumField(); i++ {
			n += memoSize(v.Field(i))
		}
		return n
	case reflect.Map:
		if v.IsNil() {
			return 0
		}
		n := int64(v.Len()) * (int64(v.Type().Key().Size()+v.Type().Elem().Size()) + 64) * 2
		iter := v.MapRange()
		for iter.Next() {
			n += memoSize(iter.Key()) + memoSize(iter.Value())
			if n > memoMaxBytes {
				return n
			}
		}
		return n
	}
	return 0
}
