package evt

import (
	"encoding/binary"
	"fmt"
	"hash"
	"reflect"

	"golang.org/x/crypto/blake2b"
)

func Hash(v interface{}) ([]byte, error) {
	h, _ := blake2b.New256(nil)

	err := hashVal(reflect.ValueOf(v), h)
	if err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

func hashVal(v reflect.Value, h hash.Hash) error {
	t := reflect.TypeOf(0)

	// Loop since these can be wrapped in multiple layers of pointers
	// and interfaces.
	for {
		// If we have an interface, dereference it. We have to do this up
		// here because it might be a nil in there and the check below must
		// catch that.
		if v.Kind() == reflect.Interface {
			v = v.Elem()
			continue
		}

		if v.Kind() == reflect.Ptr {
			v = reflect.Indirect(v)
			continue
		}

		break
	}

	// If it is nil, treat it like a zero.
	if !v.IsValid() {
		v = reflect.Zero(t)
	}

	// Binary writing can use raw ints, we have to convert to
	// a sized-int, we'll choose the largest...
	switch v.Kind() {
	case reflect.Int:
		v = reflect.ValueOf(int64(v.Int()))
	case reflect.Uint:
		v = reflect.ValueOf(uint64(v.Uint()))
	case reflect.Bool:
		var tmp int8
		if v.Bool() {
			tmp = 1
		}
		v = reflect.ValueOf(tmp)
	}

	k := v.Kind()

	// We can shortcut numeric values by directly binary writing them
	if k >= reflect.Int && k <= reflect.Complex64 {
		err := binary.Write(h, binary.LittleEndian, v.Interface())
		return err
	}

	switch k {
	case reflect.Array:
		l := v.Len()
		for i := 0; i < l; i++ {
			err := hashVal(v.Index(i), nil)
			if err != nil {
				return err
			}
		}
	case reflect.String:
		_, err := h.Write([]byte(v.String()))
		return err

	case reflect.Map:
		// Build the hash for the map. We do this by XOR-ing all the key
		// and value hashes. This makes it deterministic despite ordering.
		var agg []byte
		for _, k := range v.MapKeys() {
			v := v.MapIndex(k)

			eh, _ := blake2b.New256(nil)

			err := hashVal(k, eh)
			if err != nil {
				return err
			}

			err = hashVal(v, eh)
			if err != nil {
				return err
			}

			if h == nil {
				agg = eh.Sum(nil)
			} else {
				for i, x := range eh.Sum(nil) {
					agg[i] ^= x
				}
			}
		}

		h.Write(agg)
	case reflect.Struct:
		t := v.Type()
		err := hashVal(reflect.ValueOf(t.Name()), h)
		if err != nil {
			return err
		}

		l := v.NumField()
		for i := 0; i < l; i++ {
			if innerV := v.Field(i); v.CanSet() || t.Field(i).Name != "_" {
				fieldType := t.Field(i)
				if fieldType.PkgPath != "" {
					// Unexported
					continue
				}

				tag := fieldType.Tag.Get("hash")
				if tag == "ignore" || tag == "-" {
					// Ignore this field
					continue
				}

				err := hashVal(reflect.ValueOf(fieldType.Name), h)
				if err != nil {
					return err
				}

				err = hashVal(innerV, h)
				if err != nil {
					return err
				}
			}
		}
	case reflect.Slice:
		l := v.Len()
		for i := 0; i < l; i++ {
			err := hashVal(v.Index(i), h)
			if err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("unknown kind to hash: %s", k)
	}

	return nil
}
