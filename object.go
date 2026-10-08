package validate

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// FieldSpec binds an external field name to a schema. Create with Field.
type FieldSpec struct {
	name    string
	outType reflect.Type
	isArray bool
	eng     any // the field's engine[T], kept for introspection and codegen
	run     func(c *pctx, in input) (reflect.Value, bool)
}

// Field declares an object field. name is the external contract (the JSON key,
// query parameter, ...). It is mapped to a struct field by exact Go name, then
// by normalised name (case, '_' and '-' ignored): "email_address" ->
// EmailAddress. Ambiguity panics when the Object is built.
func Field[T any](name string, s Schema[T]) FieldSpec {
	e := toEngine(s)
	return FieldSpec{
		name:    name,
		outType: reflect.TypeOf((*T)(nil)).Elem(),
		isArray: isArrayEngine(e),
		eng:     e,
		run: func(c *pctx, in input) (reflect.Value, bool) {
			v, ok := e.run(c, in)
			return reflect.ValueOf(&v).Elem(), ok
		},
	}
}

type unknownMode uint8

const (
	unknownStrip unknownMode = iota
	unknownStrict
)

type fieldPlan struct {
	spec   FieldSpec
	index  []int
	assign func(dst, src reflect.Value)
}

// ObjectSchema validates a map-shaped input into struct T.
//
// Unknown keys are stripped (ignored) by default; Strict reports them as
// unknown_key issues. There is deliberately no "allow unknown" mode: a fixed
// struct cannot retain arbitrary keys, and pretending otherwise would hide data
// loss.
type ObjectSchema[T any] struct {
	plans   []fieldPlan
	names   map[string]struct{}
	mode    unknownMode
	refines []func(T) error
}

// Object builds a schema producing T, which must be a struct type. Fields are
// validated in the order given. Definition mistakes (duplicate names, no
// matching struct field, ambiguous mapping, output not assignable to the
// field) panic.
func Object[T any](fields ...FieldSpec) ObjectSchema[T] {
	t := reflect.TypeOf((*T)(nil)).Elem()
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("validate: Object[%s]: target must be a struct type", t))
	}
	info := structInfoFor(t)
	s := ObjectSchema[T]{names: map[string]struct{}{}}
	used := map[string]string{}
	for _, f := range fields {
		if _, dup := s.names[f.name]; dup {
			panic(fmt.Sprintf("validate: Object[%s]: duplicate field %q", t, f.name))
		}
		s.names[f.name] = struct{}{}
		sf := info.resolve(t, f.name)
		key := fmt.Sprint(sf.index)
		if prev, clash := used[key]; clash {
			panic(fmt.Sprintf("validate: Object[%s]: fields %q and %q both map to Go field %s", t, prev, f.name, sf.name))
		}
		used[key] = f.name
		assign, ok := planAssign(f.outType, sf.typ)
		if !ok {
			panic(fmt.Sprintf("validate: Object[%s]: field %q produces %s, which is not assignable to %s.%s (%s)",
				t, f.name, f.outType, t, sf.name, sf.typ))
		}
		s.plans = append(s.plans, fieldPlan{spec: f, index: sf.index, assign: assign})
	}
	return s
}

// Strict reports unknown input keys as unknown_key issues.
func (s ObjectSchema[T]) Strict() ObjectSchema[T] { s.mode = unknownStrict; return s }

// StripUnknown ignores unknown input keys (the default).
func (s ObjectSchema[T]) StripUnknown() ObjectSchema[T] { s.mode = unknownStrip; return s }

// Refine adds an object-level check, run only when every field validated. Use
// IssueAt to attribute the failure to a field.
func (s ObjectSchema[T]) Refine(fn func(T) error) ObjectSchema[T] {
	r := make([]func(T) error, len(s.refines), len(s.refines)+1)
	copy(r, s.refines)
	s.refines = append(r, fn)
	return s
}

func (s ObjectSchema[T]) run(c *pctx, in input) (T, bool) {
	var out T
	if !in.present {
		c.required(in)
		return out, false
	}
	m, ok := asStringMap(in.v)
	if !ok {
		c.typeErr(in, "object")
		return out, false
	}
	rv := reflect.ValueOf(&out).Elem()
	before := len(c.st.issues)
	for _, p := range s.plans {
		raw, present := m[p.spec.name]
		val, ok := p.spec.run(c.child(Key(p.spec.name)), input{v: raw, present: present})
		if ok {
			p.assign(fieldByIndexAlloc(rv, p.index), val)
		}
		if c.stop() {
			var zero T
			return zero, false
		}
	}
	if s.mode == unknownStrict {
		var unknown []string
		for k := range m {
			if _, known := s.names[k]; !known {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
		for _, k := range unknown {
			c.child(Key(k)).fail(input{v: m[k], present: true}, CodeUnknownKey, "is not an allowed key")
		}
	}
	if len(c.st.issues) > before {
		var zero T
		return zero, false
	}
	for _, r := range s.refines {
		if !applyRefine(c, in, out, r) {
			var zero T
			return zero, false
		}
	}
	return out, true
}

// objectFields exposes declared names and array-ness to transport adapters.
func (s ObjectSchema[T]) objectFields() []fieldShape {
	out := make([]fieldShape, len(s.plans))
	for i, p := range s.plans {
		out[i] = fieldShape{name: p.spec.name, array: p.spec.isArray}
	}
	return out
}

type fieldShape struct {
	name  string
	array bool
}

type objectShaper interface{ objectFields() []fieldShape }

// ---- reflection (isolated) ----

type structField struct {
	name  string
	norm  string
	index []int
	typ   reflect.Type
}

type structInfo struct{ fields []structField }

var structCache sync.Map // reflect.Type -> *structInfo

func normName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "")
	return strings.ReplaceAll(s, "-", "")
}

func structInfoFor(t reflect.Type) *structInfo {
	if v, ok := structCache.Load(t); ok {
		return v.(*structInfo)
	}
	info := &structInfo{}
	for _, f := range reflect.VisibleFields(t) {
		if !f.IsExported() || !settablePath(t, f.Index) {
			continue
		}
		info.fields = append(info.fields, structField{
			name: f.Name, norm: normName(f.Name), index: f.Index, typ: f.Type,
		})
	}
	v, _ := structCache.LoadOrStore(t, info)
	return v.(*structInfo)
}

// settablePath rejects promoted fields reached through an unexported embedded
// pointer, which reflect cannot allocate.
func settablePath(t reflect.Type, index []int) bool {
	cur := t
	for i, idx := range index {
		f := cur.Field(idx)
		if i < len(index)-1 {
			if f.Type.Kind() == reflect.Ptr {
				if !f.IsExported() {
					return false
				}
				cur = f.Type.Elem()
			} else {
				cur = f.Type
			}
		}
	}
	return true
}

func (si *structInfo) resolve(t reflect.Type, name string) structField {
	var exact, loose []structField
	n := normName(name)
	for _, f := range si.fields {
		if f.name == name {
			exact = append(exact, f)
		}
		if f.norm == n {
			loose = append(loose, f)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0]
	case len(exact) > 1:
		panic(fmt.Sprintf("validate: Object[%s]: field %q is ambiguous", t, name))
	case len(loose) == 1:
		return loose[0]
	case len(loose) > 1:
		names := make([]string, len(loose))
		for i, f := range loose {
			names[i] = f.name
		}
		panic(fmt.Sprintf("validate: Object[%s]: field %q is ambiguous between Go fields %s", t, name, strings.Join(names, ", ")))
	}
	panic(fmt.Sprintf("validate: Object[%s]: no exported Go field matches %q", t, name))
}

func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for i, idx := range index {
		v = v.Field(idx)
		if i < len(index)-1 && v.Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
	}
	return v
}

// planAssign decides once, at build time, how a schema's output type lands in
// a struct field: directly, unwrapped from Optional/Nullable (absent and null
// leave the zero value), or behind a pointer.
func planAssign(src, dst reflect.Type) (func(dst, src reflect.Value), bool) {
	if src.AssignableTo(dst) {
		return func(d, s reflect.Value) { d.Set(s) }, true
	}
	if src.Kind() == reflect.Struct {
		if w, ok := reflect.Zero(src).Interface().(unwrapper); ok {
			inner := w.innerType()
			next, ok := planAssign(inner, dst)
			if !ok {
				return nil, false
			}
			return func(d, s reflect.Value) {
				x, set := s.Interface().(unwrapper).unwrap()
				if !set {
					return
				}
				iv := reflect.New(inner).Elem()
				if x != nil {
					iv.Set(reflect.ValueOf(x))
				}
				next(d, iv)
			}, true
		}
	}
	if dst.Kind() == reflect.Ptr {
		next, ok := planAssign(src, dst.Elem())
		if ok {
			return func(d, s reflect.Value) {
				p := reflect.New(dst.Elem())
				next(p.Elem(), s)
				d.Set(p)
			}, true
		}
	}
	return nil, false
}
