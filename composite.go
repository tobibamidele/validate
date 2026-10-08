package validate

import (
	"fmt"
	"reflect"
	"sort"
)

// ---- Array ----

// ArraySchema validates a list and produces []T. Elements are validated in
// index order and errors carry the index in their path.
type ArraySchema[T any] struct {
	elem     engine[T]
	min, max int
	length   int
	hasMin   bool
	hasMax   bool
	hasLen   bool
	def      func() []T
}

// Array returns a schema for []T.
func Array[T any](elem Schema[T]) ArraySchema[T] { return ArraySchema[T]{elem: toEngine(elem)} }

// Min requires at least n items.
func (s ArraySchema[T]) Min(n int) ArraySchema[T] {
	mustNonNeg("Array.Min", n)
	s.min, s.hasMin = n, true
	return s
}

// Max allows at most n items.
func (s ArraySchema[T]) Max(n int) ArraySchema[T] {
	mustNonNeg("Array.Max", n)
	s.max, s.hasMax = n, true
	return s
}

// Length requires exactly n items.
func (s ArraySchema[T]) Length(n int) ArraySchema[T] {
	mustNonNeg("Array.Length", n)
	s.length, s.hasLen = n, true
	return s
}

// DefaultFunc supplies a fresh value per parse when the input is absent, so
// mutable defaults are never shared between requests.
func (s ArraySchema[T]) DefaultFunc(fn func() []T) ArraySchema[T] { s.def = fn; return s }

func (s ArraySchema[T]) isArraySchema() bool { return true }

func (s ArraySchema[T]) run(c *pctx, in input) ([]T, bool) {
	if !in.present {
		if s.def != nil {
			return s.def(), true
		}
		c.required(in)
		return nil, false
	}
	items, ok := asSlice(in.v)
	if !ok {
		c.typeErr(in, "array")
		return nil, false
	}
	good := true
	n := len(items)
	if s.hasMin && n < s.min {
		c.fail(in, CodeTooSmall, fmt.Sprintf("must contain at least %d items", s.min), meta("minimum", s.min))
		good = false
	}
	if s.hasMax && n > s.max {
		c.fail(in, CodeTooLarge, fmt.Sprintf("must contain at most %d items", s.max), meta("maximum", s.max))
		good = false
	}
	if s.hasLen && n != s.length {
		c.fail(in, CodeInvalidLength, fmt.Sprintf("must contain exactly %d items", s.length), meta("length", s.length))
		good = false
	}
	if c.stop() {
		return nil, false
	}
	out := make([]T, 0, n)
	for i, it := range items {
		v, ok := s.elem.run(c.child(Idx(i)), input{v: it, present: true})
		if !ok {
			good = false
		} else {
			out = append(out, v)
		}
		if c.stop() {
			return nil, false
		}
	}
	if !good {
		return nil, false
	}
	return out, true
}

func asSlice(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case nil, string, []byte:
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// ---- Map ----

// MapSchema validates a string-keyed map and produces map[string]T. Keys are
// processed in sorted order so issue ordering is deterministic.
type MapSchema[T any] struct {
	elem     engine[T]
	min, max int
	hasMin   bool
	hasMax   bool
	def      func() map[string]T
}

// Map returns a schema for map[string]T.
func Map[T any](elem Schema[T]) MapSchema[T] { return MapSchema[T]{elem: toEngine(elem)} }

// Min requires at least n entries.
func (s MapSchema[T]) Min(n int) MapSchema[T] {
	mustNonNeg("Map.Min", n)
	s.min, s.hasMin = n, true
	return s
}

// Max allows at most n entries.
func (s MapSchema[T]) Max(n int) MapSchema[T] {
	mustNonNeg("Map.Max", n)
	s.max, s.hasMax = n, true
	return s
}

// DefaultFunc supplies a fresh map per parse when the input is absent.
func (s MapSchema[T]) DefaultFunc(fn func() map[string]T) MapSchema[T] { s.def = fn; return s }

func (s MapSchema[T]) run(c *pctx, in input) (map[string]T, bool) {
	if !in.present {
		if s.def != nil {
			return s.def(), true
		}
		c.required(in)
		return nil, false
	}
	m, ok := asStringMap(in.v)
	if !ok {
		c.typeErr(in, "object")
		return nil, false
	}
	good := true
	if s.hasMin && len(m) < s.min {
		c.fail(in, CodeTooSmall, fmt.Sprintf("must contain at least %d entries", s.min), meta("minimum", s.min))
		good = false
	}
	if s.hasMax && len(m) > s.max {
		c.fail(in, CodeTooLarge, fmt.Sprintf("must contain at most %d entries", s.max), meta("maximum", s.max))
		good = false
	}
	keys := sortedKeys(m)
	out := make(map[string]T, len(m))
	for _, k := range keys {
		v, ok := s.elem.run(c.child(Key(k)), input{v: m[k], present: true})
		if !ok {
			good = false
		} else {
			out[k] = v
		}
		if c.stop() {
			return nil, false
		}
	}
	if !good {
		return nil, false
	}
	return out, true
}

func asStringMap(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		return x, true
	case nil:
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	out := make(map[string]any, rv.Len())
	it := rv.MapRange()
	for it.Next() {
		out[it.Key().String()] = it.Value().Interface()
	}
	return out, true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- Enum ----

// EnumSchema validates membership in a fixed set and preserves the named type
// (Enum(RoleAdmin, RoleMember) produces Role, not string).
type EnumSchema[T comparable] struct {
	values []T
	set    map[T]struct{}
	typ    reflect.Type
	def    *T
}

// Enum returns a schema accepting only the given values. It panics when no
// values are given.
func Enum[T comparable](values ...T) EnumSchema[T] {
	if len(values) == 0 {
		panic("validate: Enum: no values")
	}
	set := make(map[T]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return EnumSchema[T]{
		values: append([]T(nil), values...),
		set:    set,
		typ:    reflect.TypeOf((*T)(nil)).Elem(),
	}
}

// Default supplies a value when the input is absent.
func (s EnumSchema[T]) Default(v T) EnumSchema[T] {
	if _, ok := s.set[v]; !ok {
		panic("validate: Enum.Default: value is not a member of the enum")
	}
	s.def = &v
	return s
}

func (s EnumSchema[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	if !in.present {
		if s.def != nil {
			return *s.def, true
		}
		c.required(in)
		return zero, false
	}
	v, ok := s.convert(in.v)
	if !ok {
		c.typeErr(in, s.typ.Kind().String())
		return zero, false
	}
	if _, member := s.set[v]; !member {
		c.fail(in, CodeInvalidEnum, "is not an allowed value", meta("allowed", s.values))
		return zero, false
	}
	return v, true
}

// convert maps a raw input onto T: an exact T, a value of the same underlying
// kind (string -> named string), or an integral number for integer kinds.
func (s EnumSchema[T]) convert(v any) (T, bool) {
	var zero T
	if t, ok := v.(T); ok {
		return t, true
	}
	if v == nil {
		return zero, false
	}
	rv := reflect.ValueOf(v)
	out := reflect.New(s.typ).Elem()
	switch s.typ.Kind() {
	case reflect.String:
		if rv.Kind() != reflect.String {
			return zero, false
		}
		out.SetString(rv.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		neg, mag, kind := (IntegerSchema[int64]{signed: true, bits: 64}).toMagnitude(v)
		if kind != magOK || mag > 1<<63-1 && !neg {
			return zero, false
		}
		n := int64(mag)
		if neg {
			n = -n
		}
		if out.OverflowInt(n) {
			return zero, false
		}
		out.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		neg, mag, kind := (IntegerSchema[uint64]{bits: 64}).toMagnitude(v)
		if kind != magOK || neg || out.OverflowUint(mag) {
			return zero, false
		}
		out.SetUint(mag)
	case reflect.Bool:
		if rv.Kind() != reflect.Bool {
			return zero, false
		}
		out.SetBool(rv.Bool())
	default:
		return zero, false
	}
	return out.Interface().(T), true
}

// ---- Union ----

// UnionSchema accepts the first matching alternative. Go has no anonymous sum
// types, so the output is any; use type switches on the result, and prefer
// Enum or Object when the alternatives share a type.
type UnionSchema struct{ alts []Node }

// Union returns a schema trying each alternative in order.
func Union(alternatives ...Node) UnionSchema {
	if len(alternatives) == 0 {
		panic("validate: Union: no alternatives")
	}
	return UnionSchema{alts: append([]Node(nil), alternatives...)}
}

func (s UnionSchema) run(c *pctx, in input) (any, bool) {
	if !in.present {
		c.required(in)
		return nil, false
	}
	for _, alt := range s.alts {
		scratch := &pctx{ctx: c.ctx, parent: c.parent, seg: c.seg, st: &state{}, opts: c.opts, depth: c.depth}
		if v, ok := alt.runAny(scratch, in); ok && len(scratch.st.issues) == 0 {
			return v, true
		}
		if scratch.st.err != nil {
			c.st.err = scratch.st.err
			return nil, false
		}
	}
	c.fail(in, CodeInvalidUnion, "does not match any allowed type", withExpected("union", kindOf(in.v)))
	return nil, false
}

// ---- Transform / Custom / Refine ----

// TransformSchema runs the inner schema, then converts its output to B.
type TransformSchema[A, B any] struct {
	inner engine[A]
	fn    func(A) (B, error)
}

// Transform converts the output of a schema to another type. A failing fn
// produces a transform_error issue (or the code/message of a *RefineError it
// returns); the underlying error is kept on Issue.Cause for logs only.
func Transform[A, B any](s Schema[A], fn func(A) (B, error)) TransformSchema[A, B] {
	return TransformSchema[A, B]{inner: toEngine(s), fn: fn}
}

func (s TransformSchema[A, B]) run(c *pctx, in input) (B, bool) {
	var zero B
	a, ok := s.inner.run(c, in)
	if !ok {
		return zero, false
	}
	b, err := s.fn(a)
	if err != nil {
		var re *RefineError
		if asRefine(err, &re) {
			addRefineError(c, in, err)
		} else {
			c.fail(in, CodeTransformError, "could not be converted", func(i *Issue) { i.cause = err })
		}
		return zero, false
	}
	return b, true
}

// CustomSchema is a user-defined parser.
type CustomSchema[T any] struct{ fn func(value any) (T, []Issue) }

// Custom builds a schema from a function that receives the raw value and
// returns either a result or issues. Issue paths are relative to the value.
// An absent value is reported as required before fn is called.
func Custom[T any](fn func(value any) (T, []Issue)) CustomSchema[T] { return CustomSchema[T]{fn} }

func (s CustomSchema[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	if !in.present {
		c.required(in)
		return zero, false
	}
	v, issues := s.fn(in.v)
	if len(issues) > 0 {
		for _, is := range issues {
			is.Path = c.withPath(is.Path)
			if is.Code == "" {
				is.Code = CodeCustom
			}
			c.st.issues = append(c.st.issues, is)
		}
		return zero, false
	}
	return v, true
}

// RefineSchema adds a custom check to any schema.
type RefineSchema[T any] struct {
	inner engine[T]
	fn    func(T) error
}

// Refine adds a custom check to any schema, run only if the inner schema
// succeeded. For String and Object there are fluent Refine methods too.
func Refine[T any](s Schema[T], fn func(T) error) RefineSchema[T] {
	return RefineSchema[T]{inner: toEngine(s), fn: fn}
}

func (s RefineSchema[T]) run(c *pctx, in input) (T, bool) {
	v, ok := s.inner.run(c, in)
	if !ok {
		return v, false
	}
	if !applyRefine(c, in, v, s.fn) {
		var zero T
		return zero, false
	}
	return v, true
}
