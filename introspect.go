package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
)

// This file is the seam used by the code generator (package validate/gen) and
// by generated code. Nothing here is needed to write or use schemas by hand.

// FieldInfo describes one declared object field.
type FieldInfo struct {
	Name       string       // external name, e.g. the JSON key
	GoPath     []string     // struct field path; more than one element means a promoted embedded field
	ViaPointer bool         // GoPath crosses an embedded pointer (unsupported by the generator)
	GoType     reflect.Type // the struct field's type
	OutType    reflect.Type // what the field's schema produces
	Nested     *ObjectInfo  // non-nil when the field's schema is itself an Object
}

// ObjectInfo is the structure of an Object schema: everything the generator
// needs to unroll it, and nothing about leaf rules. Leaves keep running on the
// live runtime engines, which is what guarantees identical semantics.
type ObjectInfo struct {
	GoType  reflect.Type
	Fields  []FieldInfo
	Strict  bool
	Refines int
}

type objectInspector interface {
	inspect() ObjectInfo
	fieldEngine(name string) (any, bool)
	refinesAny() any
}

func (s ObjectSchema[T]) inspect() ObjectInfo {
	t := reflect.TypeOf((*T)(nil)).Elem()
	info := ObjectInfo{GoType: t, Strict: s.mode == unknownStrict, Refines: len(s.refines)}
	for _, p := range s.plans {
		fi := FieldInfo{Name: p.spec.name, OutType: p.spec.outType}
		cur := t
		for i, idx := range p.index {
			f := cur.Field(idx)
			fi.GoPath = append(fi.GoPath, f.Name)
			if i == len(p.index)-1 {
				fi.GoType = f.Type
			} else if f.Type.Kind() == reflect.Ptr {
				fi.ViaPointer = true
				cur = f.Type.Elem()
			} else {
				cur = f.Type
			}
		}
		if oi, ok := p.spec.eng.(objectInspector); ok {
			n := oi.inspect()
			fi.Nested = &n
		}
		info.Fields = append(info.Fields, fi)
	}
	return info
}

func (s ObjectSchema[T]) fieldEngine(name string) (any, bool) {
	for _, p := range s.plans {
		if p.spec.name == name {
			return p.spec.eng, true
		}
	}
	return nil, false
}

func (s ObjectSchema[T]) refinesAny() any { return s.refines }

// InspectObject returns the structure of an Object schema.
func InspectObject(schema any) (ObjectInfo, bool) {
	if oi, ok := schema.(objectInspector); ok {
		return oi.inspect(), true
	}
	return ObjectInfo{}, false
}

// Fingerprint hashes the structure InspectObject reports. Generated code
// embeds it and Generated compares it at startup, so code generated for an old
// shape of a schema is never used against a changed one.
func Fingerprint(schema any) (string, bool) {
	info, ok := InspectObject(schema)
	if !ok {
		return "", false
	}
	var b strings.Builder
	writeInfo(&b, info)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16]), true
}

func writeInfo(b *strings.Builder, o ObjectInfo) {
	fmt.Fprintf(b, "obj(%s|%s;strict=%t;refines=%d){", o.GoType.PkgPath(), o.GoType.String(), o.Strict, o.Refines)
	for _, f := range o.Fields {
		fmt.Fprintf(b, "%q->%s:%s<-%s;", f.Name, strings.Join(f.GoPath, "."), f.GoType.String(), f.OutType.String())
		if f.Nested != nil {
			writeInfo(b, *f.Nested)
		}
	}
	b.WriteString("}")
}

func walkObject(root any, names []string) any {
	cur := root
	for _, n := range names {
		oi, ok := cur.(objectInspector)
		if !ok {
			panic(fmt.Sprintf("validate: %T is not an Object schema (looking for %q)", cur, n))
		}
		next, ok := oi.fieldEngine(n)
		if !ok {
			panic(fmt.Sprintf("validate: no field %q (schema changed since generation? run go generate)", n))
		}
		cur = next
	}
	return cur
}

// Input is a value plus whether it was present at all. For generated code.
type Input struct {
	V       any
	Present bool
}

// Scope is a position in the input. It is the handle generated code uses to
// report issues at the right path. For generated code; hand-written schemas
// never need it.
type Scope struct{ c *pctx }

// Child descends into an object field.
func (s Scope) Child(name string) Scope { return Scope{s.c.child(Key(name))} }

// Map checks that in is a present object and returns it, reporting required
// or invalid_type otherwise, exactly like the runtime Object.
func (s Scope) Map(in Input) (map[string]any, bool) {
	ri := input{v: in.V, present: in.Present}
	if !in.Present {
		s.c.required(ri)
		return nil, false
	}
	m, ok := asStringMap(in.V)
	if !ok {
		s.c.typeErr(ri, "object")
		return nil, false
	}
	return m, true
}

// Mark returns the current issue count, to detect new issues later.
func (s Scope) Mark() int { return len(s.c.st.issues) }

// Failed reports whether issues were added since mark.
func (s Scope) Failed(mark int) bool { return len(s.c.st.issues) > mark }

// Stop reports whether validation must not continue (abort-early or a
// cancelled context).
func (s Scope) Stop() bool { return s.c.stop() }

// Unknown reports every key of m not in known as unknown_key, in sorted order.
func (s Scope) Unknown(m map[string]any, known []string) {
	var unknown []string
outer:
	for k := range m {
		for _, n := range known {
			if n == k {
				continue outer
			}
		}
		unknown = append(unknown, k)
	}
	sortStrings(unknown)
	for _, k := range unknown {
		s.c.child(Key(k)).fail(input{v: m[k], present: true}, CodeUnknownKey, "is not an allowed key")
	}
}

// RunRefine runs object-level refinements in order, stopping at the first
// failure, like the runtime Object.
func RunRefine[T any](s Scope, in Input, v T, fns []func(T) error) bool {
	ri := input{v: in.V, present: in.Present}
	for _, fn := range fns {
		if !applyRefine(s.c, ri, v, fn) {
			return false
		}
	}
	return true
}

// LeafFn runs a field's runtime engine at a scope.
type LeafFn[V any] func(Scope, Input) (V, bool)

// LeafAt returns the live runtime engine of the field at names (a path of
// external field names from root) as a function. Generated code calls this
// for every field it does not unroll, so leaf semantics are the runtime's own.
func LeafAt[V any](root any, names ...string) LeafFn[V] {
	e, ok := walkObject(root, names).(engine[V])
	if !ok {
		panic(fmt.Sprintf("validate: field %s does not produce %s (schema changed since generation? run go generate)",
			strings.Join(names, "."), reflect.TypeOf((*V)(nil)).Elem()))
	}
	return func(s Scope, in Input) (V, bool) {
		return e.run(s.c, input{v: in.V, present: in.Present})
	}
}

// RefinesAt returns the object-level refinements of the Object at names.
func RefinesAt[T any](root any, names ...string) []func(T) error {
	oi, ok := walkObject(root, names).(objectInspector)
	if !ok {
		panic("validate: RefinesAt: not an Object schema")
	}
	r, _ := oi.refinesAny().([]func(T) error)
	return r
}

// GenFunc is a generated validator for T.
type GenFunc[T any] func(Scope, Input) (T, bool)

// GeneratedSchema runs a generated validator when it matches the schema's
// current structure, and the runtime schema otherwise. Either way the result
// is the same; only speed differs.
type GeneratedSchema[T any] struct {
	rt  engine[T]
	gen GenFunc[T]
}

// Generated wraps rt with generated code. build is only called when the
// fingerprint matches rt's current structure, so stale generated code is never
// run, and never panics at startup: it simply falls back to rt. Check Active
// in a test to fail CI when someone forgot to regenerate.
func Generated[T any](rt Schema[T], fingerprint string, build func() GenFunc[T]) GeneratedSchema[T] {
	g := GeneratedSchema[T]{rt: toEngine(rt)}
	if fp, ok := Fingerprint(rt); ok && fp == fingerprint {
		g.gen = build()
	}
	return g
}

// Active reports whether the generated validator is in use (true) or the
// runtime fallback is (false, meaning the generated code is stale).
func (g GeneratedSchema[T]) Active() bool { return g.gen != nil }

func (g GeneratedSchema[T]) run(c *pctx, in input) (T, bool) {
	if g.gen != nil {
		return g.gen(Scope{c}, Input{V: in.v, Present: in.present})
	}
	return g.rt.run(c, in)
}

func (g GeneratedSchema[T]) objectFields() []fieldShape {
	if o, ok := g.rt.(objectShaper); ok {
		return o.objectFields()
	}
	panic("validate: Generated schema is not an Object; it cannot be used with Query/Headers/...")
}

// WrappedType reports the inner type when t is an OptionalValue or
// NullableValue. For the generator.
func WrappedType(t reflect.Type) (reflect.Type, bool) {
	if t.Kind() == reflect.Struct {
		if w, ok := reflect.Zero(t).Interface().(unwrapper); ok {
			return w.innerType(), true
		}
	}
	return nil, false
}
