package validate

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
)

// Float is the set of Go float types a schema can produce.
type Float interface{ ~float32 | ~float64 }

type floatOp[T Float] struct {
	kind numOpKind
	v    T
	set  []T
}

// FloatSchema validates floating-point numbers. NaN and ±Inf are always
// rejected, whether they arrive as Go values or as coerced text.
type FloatSchema[T Float] struct {
	coerce bool
	def    *T
	ops    []floatOp[T]
	bits   int
}

// Aliases for each width.
type (
	Float32Schema = FloatSchema[float32]
	Float64Schema = FloatSchema[float64]
)

func newFloat[T Float]() FloatSchema[T] {
	return FloatSchema[T]{bits: reflect.TypeOf((*T)(nil)).Elem().Bits()}
}

// Float32 returns a schema for float32.
func Float32() Float32Schema { return newFloat[float32]() }

// Float64 returns a schema for float64.
func Float64() Float64Schema { return newFloat[float64]() }

func (s FloatSchema[T]) with(o floatOp[T]) FloatSchema[T] {
	ops := make([]floatOp[T], len(s.ops), len(s.ops)+1)
	copy(ops, s.ops)
	s.ops = append(ops, o)
	return s
}

// Coerce additionally accepts numeric strings.
func (s FloatSchema[T]) Coerce() FloatSchema[T] { s.coerce = true; return s }

// Default supplies a value when the input is absent.
func (s FloatSchema[T]) Default(v T) FloatSchema[T] { s.def = &v; return s }

// Min requires value >= n.
func (s FloatSchema[T]) Min(n T) FloatSchema[T] { return s.with(floatOp[T]{kind: numMin, v: n}) }

// Max requires value <= n.
func (s FloatSchema[T]) Max(n T) FloatSchema[T] { return s.with(floatOp[T]{kind: numMax, v: n}) }

// Positive requires value > 0.
func (s FloatSchema[T]) Positive() FloatSchema[T] { return s.with(floatOp[T]{kind: numPositive}) }

// NonNegative requires value >= 0.
func (s FloatSchema[T]) NonNegative() FloatSchema[T] { return s.with(floatOp[T]{kind: numNonNegative}) }

// Negative requires value < 0.
func (s FloatSchema[T]) Negative() FloatSchema[T] { return s.with(floatOp[T]{kind: numNegative}) }

// NonPositive requires value <= 0.
func (s FloatSchema[T]) NonPositive() FloatSchema[T] { return s.with(floatOp[T]{kind: numNonPositive}) }

// MultipleOf requires value to be a multiple of n within a small relative
// tolerance (1e-9), because binary floats cannot represent most decimals
// exactly. n must be finite and non-zero.
func (s FloatSchema[T]) MultipleOf(n T) FloatSchema[T] {
	if n == 0 || math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
		panic("validate: Float.MultipleOf: n must be finite and non-zero")
	}
	return s.with(floatOp[T]{kind: numMultipleOf, v: n})
}

// OneOf restricts the value to the given constants.
func (s FloatSchema[T]) OneOf(values ...T) FloatSchema[T] {
	if len(values) == 0 {
		panic("validate: Float.OneOf: no values")
	}
	return s.with(floatOp[T]{kind: numOneOf, set: append([]T(nil), values...)})
}

func (s FloatSchema[T]) run(c *pctx, in input) (T, bool) {
	var zero, v T
	if !in.present {
		if s.def == nil {
			c.required(in)
			return zero, false
		}
		v = *s.def
	} else {
		var f float64
		switch x := in.v.(type) {
		case float64:
			f = x
		case float32:
			f = float64(x)
		case int:
			f = float64(x)
		case int8:
			f = float64(x)
		case int16:
			f = float64(x)
		case int32:
			f = float64(x)
		case int64:
			f = float64(x)
		case uint:
			f = float64(x)
		case uint8:
			f = float64(x)
		case uint16:
			f = float64(x)
		case uint32:
			f = float64(x)
		case uint64:
			f = float64(x)
		case json.Number:
			var ok bool
			if f, ok = s.parseText(c, in, string(x)); !ok {
				return zero, false
			}
		case string:
			if !s.coerce {
				c.typeErr(in, "number")
				return zero, false
			}
			var ok bool
			if f, ok = s.parseText(c, in, x); !ok {
				return zero, false
			}
		default:
			c.typeErr(in, "number")
			return zero, false
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			c.fail(in, CodeInvalidValue, "must be a finite number")
			return zero, false
		}
		if s.bits == 32 && math.Abs(f) > math.MaxFloat32 {
			c.fail(in, CodeTooLarge, "is out of range", meta("maximum", math.MaxFloat32))
			return zero, false
		}
		v = T(f)
	}
	ok := true
	for _, o := range s.ops {
		if !o.check(c, in, v) {
			ok = false
		}
	}
	if !ok {
		return zero, false
	}
	return v, true
}

func (s FloatSchema[T]) parseText(c *pctx, in input, t string) (float64, bool) {
	f, err := strconv.ParseFloat(t, s.bits)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			c.fail(in, CodeTooLarge, "is out of range")
		} else {
			c.fail(in, CodeInvalidValue, "must be a number")
		}
		return 0, false
	}
	return f, true
}

func (o floatOp[T]) check(c *pctx, in input, v T) bool {
	switch o.kind {
	case numMin:
		if v < o.v {
			c.fail(in, CodeTooSmall, fmt.Sprintf("must be at least %v", o.v), meta("minimum", o.v))
			return false
		}
	case numMax:
		if v > o.v {
			c.fail(in, CodeTooLarge, fmt.Sprintf("must be at most %v", o.v), meta("maximum", o.v))
			return false
		}
	case numPositive:
		if !(v > 0) {
			c.fail(in, CodeTooSmall, "must be positive", meta("minimum", 0, "exclusive", true))
			return false
		}
	case numNonNegative:
		if v < 0 {
			c.fail(in, CodeTooSmall, "must not be negative", meta("minimum", 0))
			return false
		}
	case numNegative:
		if !(v < 0) {
			c.fail(in, CodeTooLarge, "must be negative", meta("maximum", 0, "exclusive", true))
			return false
		}
	case numNonPositive:
		if v > 0 {
			c.fail(in, CodeTooLarge, "must not be positive", meta("maximum", 0))
			return false
		}
	case numMultipleOf:
		q := float64(v) / float64(o.v)
		if math.Abs(q-math.Round(q)) > 1e-9*math.Max(1, math.Abs(q)) {
			c.fail(in, CodeNotMultipleOf, fmt.Sprintf("must be a multiple of %v", o.v), meta("multipleOf", o.v))
			return false
		}
	case numOneOf:
		for _, x := range o.set {
			if x == v {
				return true
			}
		}
		c.fail(in, CodeInvalidValue, "is not an allowed value", meta("allowed", o.set))
		return false
	}
	return true
}
