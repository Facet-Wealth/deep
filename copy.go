package deep

import (
	"fmt"
	"reflect"
	"time"
)

// Copier is an interface that types can implement to provide their own
// custom deep copy logic. The type T in Copy() (T, error) must be the
// same concrete type as the receiver that implements this interface.
type Copier interface {
	DeepCopy() interface{}
}

// Copy creates a deep copy of src. It returns the copy and a nil error in case
// of success and the zero value for the type and a non-nil error on failure.
func Copy[T any](src T) (T, error) {
	return copyInternal(src, false)
}

// CopySkipUnsupported creates a deep copy of src. It returns the copy and a nil
// error in case of success and the zero value for the type and a non-nil error
// on failure. Unsupported types are skipped (the copy will have the zero value
// for the type) instead of returning an error.
func CopySkipUnsupported[T any](src T) (T, error) {
	return copyInternal(src, true)
}

// MustCopy creates a deep copy of src. It returns the copy on success or panics
// in case of any failure.
func MustCopy[T any](src T) T {
	dst, err := copyInternal(src, false)
	if err != nil {
		panic(err)
	}

	return dst
}

type pointersMapKey struct {
	ptr uintptr
	typ reflect.Type
}
type pointersMap map[pointersMapKey]reflect.Value

func copyInternal[T any](src T, skipUnsupported bool) (T, error) {
	v := reflect.ValueOf(src)

	// If src is the zero value for its type (e.g. an uninitialized interface,
	// or if T is 'any' and src is its zero value), v will be invalid.
	if !v.IsValid() {
		// This amounts to returning the zero value for T.
		var t T
		return t, nil
	}

	dst, err := recursiveCopy(v, make(pointersMap),
		skipUnsupported)
	if err != nil {
		var t T
		return t, err
	}

	// If we were given a plain nil value, then our dest won't be valid and calling .Interface() will panic.
	// In this situation, return the zero value for T similar to how we handle other nil pointers
	if !dst.IsValid() {
		var zero T
		return zero, nil
	}

	switch dst.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		if dst.IsNil() || dst.IsZero() {
			// The zero value for these types is nil, so this is the correct
			// and type-safe way to return nil.
			var zero T
			return zero, nil
		}
	default:
		// do nothing special
	}

	return dst.Interface().(T), nil
}

func recursiveCopy(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {

	if v.CanInterface() {
		if copier, ok := v.Interface().(Copier); ok {
			return reflect.ValueOf(copier.DeepCopy()), nil
		}
	}

	switch v.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128, reflect.String:
		// Direct type, just copy it.
		return v, nil
	case reflect.Array:
		return recursiveCopyArray(v, pointers, skipUnsupported)
	case reflect.Interface:
		return recursiveCopyInterface(v, pointers, skipUnsupported)
	case reflect.Map:
		return recursiveCopyMap(v, pointers, skipUnsupported)
	case reflect.Ptr:
		return recursiveCopyPtr(v, pointers, skipUnsupported)
	case reflect.Slice:
		return recursiveCopySlice(v, pointers, skipUnsupported)
	case reflect.Struct:
		return recursiveCopyStruct(v, pointers, skipUnsupported)
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		if v.IsNil() {
			// If we have a nil function, unsafe pointer or channel, then we
			// can copy it.
			return v, nil
		} else {
			if skipUnsupported {
				return reflect.Zero(v.Type()), nil
			} else {
				return reflect.Value{}, fmt.Errorf("unsuported non-nil value for type: %s", v.Type())
			}
		}
	default:
		if skipUnsupported {
			return reflect.Zero(v.Type()), nil
		} else {
			return reflect.Value{}, fmt.Errorf("unsuported type: %s", v.Type())
		}
	}
}

func recursiveCopyArray(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {
	dst := reflect.New(v.Type()).Elem()

	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		elemDst, err := recursiveCopy(elem, pointers, skipUnsupported)
		if err != nil {
			return reflect.Value{}, err
		}

		dst.Index(i).Set(elemDst)
	}

	return dst, nil
}

func recursiveCopyInterface(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {
	if v.IsNil() {
		// If the interface is nil, just return it.
		return v, nil
	}

	return recursiveCopy(v.Elem(), pointers, skipUnsupported)
}

func recursiveCopyMap(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {
	if v.IsNil() {
		// If the slice is nil, just return it.
		return v, nil
	}

	dst := reflect.MakeMap(v.Type())

	for _, key := range v.MapKeys() {
		elem := v.MapIndex(key)
		elemDst, err := recursiveCopy(elem, pointers,
			skipUnsupported)
		if err != nil {
			return reflect.Value{}, err
		}

		dst.SetMapIndex(key, elemDst)
	}

	return dst, nil
}

func recursiveCopyPtr(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {
	// If the pointer is nil, just return it.
	if v.IsNil() {
		return v, nil
	}

	ptr := v.Pointer()
	typ := v.Type()
	key := pointersMapKey{ptr, typ}

	// If the pointer is already in the pointers map, return it.
	if dst, ok := pointers[key]; ok {
		return dst, nil
	}

	// Otherwise, create a new pointer and add it to the pointers map.
	dst := reflect.New(v.Type().Elem())

	pointers[key] = dst

	// Proceed with the copy.
	elem := v.Elem()
	elemDst, err := recursiveCopy(elem, pointers, skipUnsupported)
	if err != nil {
		return reflect.Value{}, err
	}

	dst.Elem().Set(elemDst)

	return dst, nil
}

func recursiveCopySlice(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {
	if v.IsNil() {
		// If the slice is nil, just return it.
		return v, nil
	}

	dst := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())

	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		elemDst, err := recursiveCopy(elem, pointers,
			skipUnsupported)
		if err != nil {
			return reflect.Value{}, err
		}

		dst.Index(i).Set(elemDst)
	}

	return dst, nil
}

func recursiveCopyStruct(v reflect.Value, pointers pointersMap,
	skipUnsupported bool) (reflect.Value, error) {
	dst := reflect.New(v.Type()).Elem()

	t, ok := v.Interface().(time.Time)
	if ok {
		dst.Set(reflect.ValueOf(t))
		return dst, nil
	}

	for i := 0; i < v.NumField(); i++ {
		elem := v.Field(i)

		// The Type's StructField for a given field is checked to see if StructField.PkgPath
		// is set to determine if the field is exported or not because CanSet() returns false
		// for settable fields
		if v.Type().Field(i).PkgPath != "" {
			continue
		}

		elemDst, err := recursiveCopy(elem, pointers,
			skipUnsupported)
		if err != nil {
			return reflect.Value{}, err
		}

		dstField := dst.Field(i)

		dstField.Set(elemDst)
	}

	return dst, nil
}
