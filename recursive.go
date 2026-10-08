package validate

import (
	"fmt"
	"sync"
)

// DefaultMaxDepth is how many nested Recursive/Lazy levels a parse may enter
// before failing with too_deep. It bounds stack and CPU use on hostile input
// and on cyclic Go values passed to Parse. Override with WithMaxDepth.
const DefaultMaxDepth = 128

// WithMaxDepth sets the recursion limit for one parse. n <= 0 restores the
// default.
func WithMaxDepth(n int) Option { return func(o *options) { o.maxDepth = n } }

func (c *pctx) enter(in input) (*pctx, bool) {
	limit := c.opts.maxDepth
	if limit <= 0 {
		limit = DefaultMaxDepth
	}
	if c.depth >= limit {
		c.fail(in, CodeTooDeep, fmt.Sprintf("is nested more than %d levels deep", limit), meta("maximum", limit))
		return nil, false
	}
	cc := *c
	cc.depth++
	return &cc, true
}

// selfRef is the handle Recursive passes to its builder. It implements
// Schema[T], so it can be used anywhere a schema can.
type selfRef[T any] struct{ inner engine[T] }

func (r *selfRef[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	if r.inner == nil {
		panic("validate: Recursive: self reference used before the builder returned")
	}
	cc, ok := c.enter(in)
	if !ok {
		return zero, false
	}
	return r.inner.run(cc, in)
}

func (r *selfRef[T]) Parse(v any) (T, error)               { return parseVia[T](r, v) }
func (r *selfRef[T]) SafeParse(v any) Result[T]            { return safeVia[T](r, v) }
func (r *selfRef[T]) runAny(c *pctx, in input) (any, bool) { return anyVia[T](r, c, in) }

// RecursiveSchema is a schema that refers to itself.
type RecursiveSchema[T any] struct{ ref *selfRef[T] }

// Recursive builds a self-referential schema. build receives a reference to
// the schema being defined; use it wherever the type nests:
//
//	type Comment struct {
//	    Body    string
//	    Replies []Comment
//	}
//
//	var CommentSchema = validate.Recursive(func(self validate.Schema[Comment]) validate.Schema[Comment] {
//	    return validate.Object[Comment](
//	        validate.Field("body", validate.String().Min(1)),
//	        validate.Field("replies", validate.Array(self).DefaultFunc(func() []Comment { return nil })),
//	    )
//	})
//
// This avoids Go's "initialization cycle" error that a package-level variable
// referring to itself would cause. Every nested level counts against
// DefaultMaxDepth.
func Recursive[T any](build func(self Schema[T]) Schema[T]) RecursiveSchema[T] {
	ref := &selfRef[T]{}
	ref.inner = toEngine(build(ref))
	return RecursiveSchema[T]{ref: ref}
}

func (s RecursiveSchema[T]) run(c *pctx, in input) (T, bool) { return s.ref.inner.run(c, in) }

func (s RecursiveSchema[T]) objectFields() []fieldShape {
	if o, ok := s.ref.inner.(objectShaper); ok {
		return o.objectFields()
	}
	panic("validate: Recursive schema is not an Object; it cannot be used with Query/Headers/...")
}

// LazySchema defers building its schema to first use. Use it for mutually
// recursive schemas, built inside a function or init (a package-level
// variable that refers to itself, even through a closure, is a Go
// initialization cycle).
type LazySchema[T any] struct {
	state *lazyState[T]
}

type lazyState[T any] struct {
	once sync.Once
	fn   func() Schema[T]
	e    engine[T]
}

// Lazy defers resolving a schema until it is first used. fn runs at most once
// and must not return nil. Each use counts one level against the depth limit.
func Lazy[T any](fn func() Schema[T]) LazySchema[T] {
	if fn == nil {
		panic("validate: Lazy: nil function")
	}
	return LazySchema[T]{state: &lazyState[T]{fn: fn}}
}

func (s LazySchema[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	s.state.once.Do(func() {
		sch := s.state.fn()
		if sch == nil {
			panic("validate: Lazy: function returned a nil schema")
		}
		s.state.e = toEngine(sch)
	})
	cc, ok := c.enter(in)
	if !ok {
		return zero, false
	}
	return s.state.e.run(cc, in)
}
