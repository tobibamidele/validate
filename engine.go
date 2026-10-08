package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// Schema is a typed validator/decoder. Every schema in this package satisfies
// it, and the type parameter is preserved by every constraint method.
type Schema[T any] interface {
	Parse(value any) (T, error)
	SafeParse(value any) Result[T]
}

// Node is a schema with its output type erased. It exists so that Union can
// accept schemas of different output types. All built-in schemas are Nodes;
// wrap your own Schema[T] with AsNode.
type Node interface {
	runAny(c *pctx, in input) (any, bool)
}

// Option changes how a single parse call executes. Options are per call and
// never mutate a schema.
type Option func(*options)

type options struct {
	abortEarly    bool
	includeValues bool
	maxDepth      int
}

// WithAbortEarly stops validation at the first issue.
func WithAbortEarly() Option { return func(o *options) { o.abortEarly = true } }

// WithValues populates Issue.Value with the raw offending input. Off by
// default because inputs may be secrets.
func WithValues() Option { return func(o *options) { o.includeValues = true } }

// input is a value together with whether it was present at all, which is how
// absent and null are told apart.
type input struct {
	v       any
	present bool
}

type state struct {
	issues []Issue
	err    error // context cancellation
}

// pctx is one position in the input. Paths are lazy: a child only records its
// parent and one segment, and the full Path is built when an issue is reported,
// so successful fields allocate no path at all.
type pctx struct {
	ctx    context.Context
	parent *pctx
	seg    Segment
	st     *state
	opts   options
	depth  int
}

// fullPath materialises the path from the root. Never nil.
func (c *pctx) fullPath() Path {
	n := 0
	for p := c; p.parent != nil; p = p.parent {
		n++
	}
	out := make(Path, n)
	for p := c; p.parent != nil; p = p.parent {
		n--
		out[n] = p.seg
	}
	return out
}

func (c *pctx) withPath(rel Path) Path {
	base := c.fullPath()
	return append(base, rel...)
}

func (c *pctx) child(seg Segment) *pctx {
	return &pctx{ctx: c.ctx, parent: c, seg: seg, st: c.st, opts: c.opts, depth: c.depth}
}

// stop reports whether validation should not continue.
func (c *pctx) stop() bool {
	if c.st.err != nil {
		return true
	}
	if err := c.ctx.Err(); err != nil {
		c.st.err = err
		return true
	}
	return c.opts.abortEarly && len(c.st.issues) > 0
}

func (c *pctx) fail(in input, code, msg string, mods ...func(*Issue)) {
	is := Issue{Path: c.fullPath(), Code: code, Message: msg}
	if c.opts.includeValues && in.present {
		is.Value = in.v
	}
	for _, m := range mods {
		m(&is)
	}
	c.st.issues = append(c.st.issues, is)
}

func meta(kv ...any) func(*Issue) {
	return func(i *Issue) {
		i.Metadata = map[string]any{}
		for n := 0; n+1 < len(kv); n += 2 {
			i.Metadata[kv[n].(string)] = kv[n+1]
		}
	}
}

func withExpected(exp, got string) func(*Issue) {
	return func(i *Issue) { i.Expected, i.Received = exp, got }
}

func (c *pctx) required(in input) {
	c.fail(in, CodeRequired, "is required")
}

func (c *pctx) typeErr(in input, expected string) {
	got := kindOf(in.v)
	c.fail(in, CodeInvalidType, fmt.Sprintf("expected %s, received %s", expected, got), withExpected(expected, got))
}

func kindOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "number"
	case []byte:
		return "bytes"
	case time.Time:
		return "time"
	case map[string]any:
		return "object"
	default:
		switch reflect.ValueOf(x).Kind() {
		case reflect.Slice, reflect.Array:
			return "array"
		case reflect.Map, reflect.Struct:
			return "object"
		}
		return fmt.Sprintf("%T", v)
	}
}

// engine is the internal contract every schema implements.
type engine[T any] interface {
	run(c *pctx, in input) (T, bool)
}

func buildOptions(opts []Option) options {
	var o options
	for _, f := range opts {
		f(&o)
	}
	return o
}

func parseCtx[T any](ctx context.Context, e engine[T], v any, o options) (T, error) {
	var zero T
	st := &state{}
	out, ok := e.run(&pctx{ctx: ctx, st: st, opts: o}, input{v: v, present: true})
	if st.err != nil {
		return zero, st.err
	}
	if !ok || len(st.issues) > 0 {
		if len(st.issues) == 0 {
			st.issues = append(st.issues, Issue{Path: Path{}, Code: CodeInvalidValue, Message: "is invalid"})
		}
		return zero, &ValidationError{Issues: st.issues}
	}
	return out, nil
}

func parseVia[T any](e engine[T], v any) (T, error) {
	return parseCtx[T](context.Background(), e, v, options{})
}

func safeVia[T any](e engine[T], v any) Result[T] {
	out, err := parseVia[T](e, v)
	if err != nil {
		ve, _ := AsValidationError(err)
		return Result[T]{Error: ve}
	}
	return Result[T]{Value: out, OK: true}
}

func anyVia[T any](e engine[T], c *pctx, in input) (any, bool) {
	v, ok := e.run(c, in)
	return v, ok
}

// ParseContext parses value with a context and per-call options. It returns
// ctx.Err() if the context is cancelled mid-parse.
func ParseContext[T any](ctx context.Context, s Schema[T], value any, opts ...Option) (T, error) {
	return parseCtx[T](ctx, toEngine(s), value, buildOptions(opts))
}

// external adapts a user-defined Schema[T] into the engine.
type external[T any] struct{ s Schema[T] }

func (x external[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	if !in.present {
		c.required(in)
		return zero, false
	}
	out, err := x.s.Parse(in.v)
	if err == nil {
		return out, true
	}
	if ve, ok := AsValidationError(err); ok {
		for _, is := range ve.Issues {
			is.Path = c.withPath(is.Path)
			c.st.issues = append(c.st.issues, is)
		}
		return zero, false
	}
	c.fail(in, CodeCustom, "is invalid", func(i *Issue) { i.cause = err })
	return zero, false
}

func toEngine[T any](s Schema[T]) engine[T] {
	if e, ok := s.(engine[T]); ok {
		return e
	}
	return external[T]{s}
}

// AsNode erases the output type of any Schema so it can be used in Union.
func AsNode[T any](s Schema[T]) Node { return nodeOf[T]{toEngine(s)} }

type nodeOf[T any] struct{ e engine[T] }

func (n nodeOf[T]) runAny(c *pctx, in input) (any, bool) { return anyVia[T](n.e, c, in) }

// applyRefine runs a user refinement and converts its error into issues.
func applyRefine[T any](c *pctx, in input, v T, fn func(T) error) bool {
	err := fn(v)
	if err == nil {
		return true
	}
	addRefineError(c, in, err)
	return false
}

func addRefineError(c *pctx, in input, err error) {
	var re *RefineError
	if asRefine(err, &re) {
		is := Issue{Path: c.withPath(re.Path), Code: re.Code, Message: re.Message, Metadata: re.Metadata}
		if c.opts.includeValues && in.present {
			is.Value = in.v
		}
		c.st.issues = append(c.st.issues, is)
		return
	}
	c.fail(in, CodeCustom, err.Error(), func(i *Issue) { i.cause = err })
}
