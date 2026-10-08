package validate

import "reflect"

// OptionalValue distinguishes "absent" from "present". It is what Optional
// produces, and the right field type for PATCH-style DTOs.
type OptionalValue[T any] struct {
	v   T
	set bool
}

// Some returns a set OptionalValue.
func Some[T any](v T) OptionalValue[T] { return OptionalValue[T]{v: v, set: true} }

// IsSet reports whether the value was present in the input.
func (o OptionalValue[T]) IsSet() bool { return o.set }

// Value returns the value and whether it was set.
func (o OptionalValue[T]) Value() (T, bool) { return o.v, o.set }

// GetOr returns the value, or d when absent.
func (o OptionalValue[T]) GetOr(d T) T {
	if o.set {
		return o.v
	}
	return d
}

// NullableValue distinguishes null from a concrete value. Its zero value is
// null.
type NullableValue[T any] struct {
	v     T
	valid bool
}

// Valued returns a non-null NullableValue.
func Valued[T any](v T) NullableValue[T] { return NullableValue[T]{v: v, valid: true} }

// IsNull reports whether the input was null.
func (n NullableValue[T]) IsNull() bool { return !n.valid }

// Value returns the value and whether it is non-null.
func (n NullableValue[T]) Value() (T, bool) { return n.v, n.valid }

// GetOr returns the value, or d when null.
func (n NullableValue[T]) GetOr(d T) T {
	if n.valid {
		return n.v
	}
	return d
}

// unwrapper lets the object engine assign Optional/Nullable outputs to plain
// struct fields, pointers or the wrapper types themselves.
type unwrapper interface {
	innerType() reflect.Type
	unwrap() (any, bool)
}

func (o OptionalValue[T]) innerType() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }
func (o OptionalValue[T]) unwrap() (any, bool)     { return o.v, o.set }
func (n NullableValue[T]) innerType() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }
func (n NullableValue[T]) unwrap() (any, bool)     { return n.v, n.valid }

// OptionalSchema accepts absence. Null is passed to the inner schema, so
// Optional(String()) rejects null while Optional(Nullable(String())) accepts it.
type OptionalSchema[T any] struct{ inner engine[T] }

// Optional makes a schema accept an absent value.
func Optional[T any](s Schema[T]) OptionalSchema[T] { return OptionalSchema[T]{toEngine(s)} }

func (s OptionalSchema[T]) run(c *pctx, in input) (OptionalValue[T], bool) {
	if !in.present {
		return OptionalValue[T]{}, true
	}
	v, ok := s.inner.run(c, in)
	return OptionalValue[T]{v: v, set: true}, ok
}

func (s OptionalSchema[T]) isArraySchema() bool { return isArrayEngine(s.inner) }

// NullableSchema accepts null. Absence is still an error.
type NullableSchema[T any] struct{ inner engine[T] }

// Nullable makes a schema accept an explicit null.
func Nullable[T any](s Schema[T]) NullableSchema[T] { return NullableSchema[T]{toEngine(s)} }

func (s NullableSchema[T]) run(c *pctx, in input) (NullableValue[T], bool) {
	if !in.present {
		c.required(in)
		return NullableValue[T]{}, false
	}
	if in.v == nil {
		return NullableValue[T]{}, true
	}
	v, ok := s.inner.run(c, in)
	return NullableValue[T]{v: v, valid: true}, ok
}

func (s NullableSchema[T]) isArraySchema() bool { return isArrayEngine(s.inner) }

type arrayShaper interface{ isArraySchema() bool }

func isArrayEngine(e any) bool {
	if a, ok := e.(arrayShaper); ok {
		return a.isArraySchema()
	}
	return false
}
