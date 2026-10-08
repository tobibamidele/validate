package validate

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Integer is the set of Go integer types a schema can produce.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type numOpKind uint8

const (
	numMin numOpKind = iota
	numMax
	numPositive
	numNonNegative
	numNegative
	numNonPositive
	numMultipleOf
	numOneOf
)

type intOp[T Integer] struct {
	kind numOpKind
	v    T
	set  []T
}

// IntegerSchema validates integers of exact Go type T. Int, Int8 ... Uint64
// are aliases of its instantiations. JSON integers are accepted exactly (no
// float64 round trip); 18.5 and 18.0 are rejected; out-of-range values are
// rejected, never wrapped.
type IntegerSchema[T Integer] struct {
	coerce bool
	def    *T
	ops    []intOp[T]
	signed bool
	bits   int
}

// Aliases for each width.
type (
	IntSchema    = IntegerSchema[int]
	Int8Schema   = IntegerSchema[int8]
	Int16Schema  = IntegerSchema[int16]
	Int32Schema  = IntegerSchema[int32]
	Int64Schema  = IntegerSchema[int64]
	UintSchema   = IntegerSchema[uint]
	Uint8Schema  = IntegerSchema[uint8]
	Uint16Schema = IntegerSchema[uint16]
	Uint32Schema = IntegerSchema[uint32]
	Uint64Schema = IntegerSchema[uint64]
)

func newInteger[T Integer]() IntegerSchema[T] {
	t := reflect.TypeOf((*T)(nil)).Elem()
	k := t.Kind()
	return IntegerSchema[T]{
		signed: k >= reflect.Int && k <= reflect.Int64,
		bits:   t.Bits(),
	}
}

// Int returns a schema for int.
func Int() IntSchema { return newInteger[int]() }

// Int8 returns a schema for int8.
func Int8() Int8Schema { return newInteger[int8]() }

// Int16 returns a schema for int16.
func Int16() Int16Schema { return newInteger[int16]() }

// Int32 returns a schema for int32.
func Int32() Int32Schema { return newInteger[int32]() }

// Int64 returns a schema for int64.
func Int64() Int64Schema { return newInteger[int64]() }

// Uint returns a schema for uint.
func Uint() UintSchema { return newInteger[uint]() }

// Uint8 returns a schema for uint8.
func Uint8() Uint8Schema { return newInteger[uint8]() }

// Uint16 returns a schema for uint16.
func Uint16() Uint16Schema { return newInteger[uint16]() }

// Uint32 returns a schema for uint32.
func Uint32() Uint32Schema { return newInteger[uint32]() }

// Uint64 returns a schema for uint64.
func Uint64() Uint64Schema { return newInteger[uint64]() }

func (s IntegerSchema[T]) with(o intOp[T]) IntegerSchema[T] {
	ops := make([]intOp[T], len(s.ops), len(s.ops)+1)
	copy(ops, s.ops)
	s.ops = append(ops, o)
	return s
}

// Coerce additionally accepts base-10 strings such as "18". Surrounding
// whitespace is not trimmed.
func (s IntegerSchema[T]) Coerce() IntegerSchema[T] { s.coerce = true; return s }

// Default supplies a value when the input is absent. An explicit 0 is kept.
func (s IntegerSchema[T]) Default(v T) IntegerSchema[T] { s.def = &v; return s }

// Min requires value >= n.
func (s IntegerSchema[T]) Min(n T) IntegerSchema[T] { return s.with(intOp[T]{kind: numMin, v: n}) }

// Max requires value <= n.
func (s IntegerSchema[T]) Max(n T) IntegerSchema[T] { return s.with(intOp[T]{kind: numMax, v: n}) }

// Positive requires value > 0.
func (s IntegerSchema[T]) Positive() IntegerSchema[T] { return s.with(intOp[T]{kind: numPositive}) }

// NonNegative requires value >= 0.
func (s IntegerSchema[T]) NonNegative() IntegerSchema[T] {
	return s.with(intOp[T]{kind: numNonNegative})
}

// Negative requires value < 0. It panics for unsigned types, where it could
// never succeed.
func (s IntegerSchema[T]) Negative() IntegerSchema[T] {
	s.mustSigned("Negative")
	return s.with(intOp[T]{kind: numNegative})
}

// NonPositive requires value <= 0. It panics for unsigned types.
func (s IntegerSchema[T]) NonPositive() IntegerSchema[T] {
	s.mustSigned("NonPositive")
	return s.with(intOp[T]{kind: numNonPositive})
}

// MultipleOf requires value % n == 0. It panics when n is 0.
func (s IntegerSchema[T]) MultipleOf(n T) IntegerSchema[T] {
	if n == 0 {
		panic("validate: Integer.MultipleOf: n must not be zero")
	}
	return s.with(intOp[T]{kind: numMultipleOf, v: n})
}

// OneOf restricts the value to the given constants.
func (s IntegerSchema[T]) OneOf(values ...T) IntegerSchema[T] {
	if len(values) == 0 {
		panic("validate: Integer.OneOf: no values")
	}
	return s.with(intOp[T]{kind: numOneOf, set: append([]T(nil), values...)})
}

func (s IntegerSchema[T]) mustSigned(what string) {
	if !s.signed {
		panic("validate: Integer." + what + ": impossible for an unsigned type")
	}
}

func (s IntegerSchema[T]) run(c *pctx, in input) (T, bool) {
	var zero T
	var v T
	if !in.present {
		if s.def == nil {
			c.required(in)
			return zero, false
		}
		v = *s.def
	} else {
		neg, mag, kind := s.toMagnitude(in.v)
		switch kind {
		case magOK:
		case magType:
			c.typeErr(in, "integer")
			return zero, false
		case magFormat:
			c.fail(in, CodeInvalidValue, "must be an integer")
			return zero, false
		case magOverflow:
			if neg {
				c.fail(in, CodeTooSmall, "is out of range")
			} else {
				c.fail(in, CodeTooLarge, "is out of range")
			}
			return zero, false
		}
		var ok bool
		v, ok = s.fromMagnitude(c, in, neg, mag)
		if !ok {
			return zero, false
		}
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

const (
	magOK = iota
	magType
	magFormat
	magOverflow
)

func (s IntegerSchema[T]) toMagnitude(v any) (neg bool, mag uint64, kind int) {
	i64 := func(x int64) (bool, uint64, int) {
		if x < 0 {
			return true, uint64(-(x + 1)) + 1, magOK
		}
		return false, uint64(x), magOK
	}
	switch x := v.(type) {
	case int:
		return i64(int64(x))
	case int8:
		return i64(int64(x))
	case int16:
		return i64(int64(x))
	case int32:
		return i64(int64(x))
	case int64:
		return i64(x)
	case uint:
		return false, uint64(x), magOK
	case uint8:
		return false, uint64(x), magOK
	case uint16:
		return false, uint64(x), magOK
	case uint32:
		return false, uint64(x), magOK
	case uint64:
		return false, x, magOK
	case float32:
		return floatMag(float64(x))
	case float64:
		return floatMag(x)
	case json.Number:
		return parseIntText(string(x))
	case string:
		if s.coerce {
			return parseIntText(x)
		}
	}
	return false, 0, magType
}

// floatMag accepts a Go float only when it is a finite integer exactly
// representable in 64 bits.
func floatMag(f float64) (bool, uint64, int) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) >= 1<<63 {
		return false, 0, magFormat
	}
	if f < 0 {
		return true, uint64(-f), magOK
	}
	return false, uint64(f), magOK
}

// parseIntText parses an optionally signed run of decimal digits. Exponents
// and fractions (18.0, 1e3) are rejected.
func parseIntText(t string) (bool, uint64, int) {
	neg := false
	d := t
	if strings.HasPrefix(d, "-") {
		neg, d = true, d[1:]
	} else if strings.HasPrefix(d, "+") {
		d = d[1:]
	}
	if d == "" {
		return false, 0, magFormat
	}
	for i := 0; i < len(d); i++ {
		if d[i] < '0' || d[i] > '9' {
			return false, 0, magFormat
		}
	}
	m, err := strconv.ParseUint(d, 10, 64)
	if err != nil {
		// Digits only, so this is a 64-bit overflow.
		return neg, 0, magOverflow
	}
	return neg, m, magOK
}

func (s IntegerSchema[T]) fromMagnitude(c *pctx, in input, neg bool, mag uint64) (T, bool) {
	var zero T
	b := uint(s.bits)
	if s.signed {
		limit := uint64(1) << (b - 1) // magnitude of the minimum
		if neg && mag > limit {
			c.fail(in, CodeTooSmall, fmt.Sprintf("must be at least %d", minOf(b)), meta("minimum", minOf(b)))
			return zero, false
		}
		if !neg && mag > limit-1 {
			c.fail(in, CodeTooLarge, fmt.Sprintf("must be at most %d", int64(limit-1)), meta("maximum", int64(limit-1)))
			return zero, false
		}
		if neg {
			return T(int64(0) - int64(mag)), true // wraps correctly for MinInt64
		}
		return T(mag), true
	}
	if neg && mag != 0 {
		c.fail(in, CodeTooSmall, "must be at least 0", meta("minimum", 0))
		return zero, false
	}
	var max uint64 = math.MaxUint64
	if b < 64 {
		max = uint64(1)<<b - 1
	}
	if mag > max {
		c.fail(in, CodeTooLarge, fmt.Sprintf("must be at most %d", max), meta("maximum", max))
		return zero, false
	}
	return T(mag), true
}

func minOf(bits uint) int64 { return -int64(uint64(1)<<(bits-1)-1) - 1 }

func (o intOp[T]) check(c *pctx, in input, v T) bool {
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
		if v%o.v != 0 {
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
