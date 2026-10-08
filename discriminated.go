package validate

import (
	"fmt"
	"sort"
)

// VariantSpec binds one discriminator value to a schema. Create with Variant.
type VariantSpec[T any] struct {
	tag string
	run func(c *pctx, in input) (T, bool)
}

// Variant declares one case of a discriminated union: when the discriminator
// equals tag, the whole object is validated with s and the result is converted
// to the union type T by wrap.
//
// The discriminator key is left in the object passed to s. A Strict variant
// therefore needs to declare it (for example Field("type", String().OneOf("x")))
// or it will report unknown_key.
func Variant[T, V any](tag string, s Schema[V], wrap func(V) T) VariantSpec[T] {
	e := toEngine(s)
	return VariantSpec[T]{
		tag: tag,
		run: func(c *pctx, in input) (T, bool) {
			v, ok := e.run(c, in)
			if !ok {
				var zero T
				return zero, false
			}
			return wrap(v), true
		},
	}
}

// DiscriminatedSchema selects a variant from a string discriminator field and
// produces T, typically an interface implemented by every variant type. Unlike
// Union, only the selected variant is validated, so errors point at the real
// problem instead of "matches no alternative".
type DiscriminatedSchema[T any] struct {
	key      string
	variants map[string]VariantSpec[T]
	tags     []string // declaration order, for stable messages
}

// Discriminated builds a union keyed on the string field key. It panics on an
// empty key, no variants or duplicate tags.
func Discriminated[T any](key string, variants ...VariantSpec[T]) DiscriminatedSchema[T] {
	if key == "" {
		panic("validate: Discriminated: empty discriminator key")
	}
	if len(variants) == 0 {
		panic("validate: Discriminated: no variants")
	}
	s := DiscriminatedSchema[T]{key: key, variants: make(map[string]VariantSpec[T], len(variants))}
	for _, v := range variants {
		if _, dup := s.variants[v.tag]; dup {
			panic(fmt.Sprintf("validate: Discriminated: duplicate tag %q", v.tag))
		}
		s.variants[v.tag] = v
		s.tags = append(s.tags, v.tag)
	}
	return s
}

func (s DiscriminatedSchema[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	if !in.present {
		c.required(in)
		return zero, false
	}
	m, ok := asStringMap(in.v)
	if !ok {
		c.typeErr(in, "object")
		return zero, false
	}
	kc := c.child(Key(s.key))
	raw, present := m[s.key]
	if !present {
		kc.required(input{})
		return zero, false
	}
	tag, isStr := raw.(string)
	if !isStr {
		kc.typeErr(input{v: raw, present: true}, "string")
		return zero, false
	}
	v, known := s.variants[tag]
	if !known {
		allowed := append([]string(nil), s.tags...)
		sort.Strings(allowed)
		kc.fail(input{v: raw, present: true}, CodeInvalidUnion, "is not an allowed variant", meta("allowed", allowed))
		return zero, false
	}
	return v.run(c, in)
}
