package validate

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type strOpKind uint8

const (
	opTrim strOpKind = iota
	opLower
	opUpper
	opMin
	opMax
	opLength
	opEmail
	opURL
	opUUID
	opRegex
	opStartsWith
	opEndsWith
	opContains
	opOneOf
)

type strOp struct {
	kind strOpKind
	n    int
	s    string
	set  []string
	re   *regexp.Regexp
}

func (o strOp) normalizer() bool { return o.kind <= opUpper }

// StringSchema validates strings. Execution order is: type check, then all
// normalising operations (Trim, Lower, Upper) in declaration order, then all
// constraints in declaration order, then refinements. So
// String().Trim().Lower().Min(3).Email() measures the trimmed, lowered value.
// Min/Max/Length count runes, not bytes.
type StringSchema struct {
	ops     []strOp
	def     *string
	refines []func(string) error
}

// String returns a schema for strings.
func String() StringSchema { return StringSchema{} }

func (s StringSchema) with(o strOp) StringSchema {
	ops := make([]strOp, len(s.ops), len(s.ops)+1)
	copy(ops, s.ops)
	s.ops = append(ops, o)
	return s
}

// Trim strips leading and trailing Unicode whitespace.
func (s StringSchema) Trim() StringSchema { return s.with(strOp{kind: opTrim}) }

// Lower lowercases the value.
func (s StringSchema) Lower() StringSchema { return s.with(strOp{kind: opLower}) }

// Upper uppercases the value.
func (s StringSchema) Upper() StringSchema { return s.with(strOp{kind: opUpper}) }

// Min requires at least n runes.
func (s StringSchema) Min(n int) StringSchema {
	mustNonNeg("String.Min", n)
	return s.with(strOp{kind: opMin, n: n})
}

// Max allows at most n runes.
func (s StringSchema) Max(n int) StringSchema {
	mustNonNeg("String.Max", n)
	return s.with(strOp{kind: opMax, n: n})
}

// Length requires exactly n runes.
func (s StringSchema) Length(n int) StringSchema {
	mustNonNeg("String.Length", n)
	return s.with(strOp{kind: opLength, n: n})
}

// Email checks address syntax only: a single bare addr-spec with a dotted
// domain. No DNS lookups, no mailbox verification. Quoted local parts and
// display names are rejected.
func (s StringSchema) Email() StringSchema { return s.with(strOp{kind: opEmail}) }

// URL requires an absolute URL with a scheme and a host.
func (s StringSchema) URL() StringSchema { return s.with(strOp{kind: opURL}) }

// UUID requires the canonical 36-character UUID text form; the output stays a
// string. Use the UUID schema to get a uuid.UUID.
func (s StringSchema) UUID() StringSchema { return s.with(strOp{kind: opUUID}) }

// Regex requires the value to match pattern. The pattern is compiled once
// here; an invalid pattern panics.
func (s StringSchema) Regex(pattern string) StringSchema {
	re, err := regexp.Compile(pattern)
	if err != nil {
		panic(fmt.Sprintf("validate: String.Regex: invalid pattern %q: %v", pattern, err))
	}
	return s.with(strOp{kind: opRegex, re: re, s: pattern})
}

// StartsWith requires a prefix.
func (s StringSchema) StartsWith(prefix string) StringSchema {
	return s.with(strOp{kind: opStartsWith, s: prefix})
}

// EndsWith requires a suffix.
func (s StringSchema) EndsWith(suffix string) StringSchema {
	return s.with(strOp{kind: opEndsWith, s: suffix})
}

// Contains requires a substring.
func (s StringSchema) Contains(sub string) StringSchema {
	return s.with(strOp{kind: opContains, s: sub})
}

// OneOf restricts the value to the given constants. Use Enum for a named type.
func (s StringSchema) OneOf(values ...string) StringSchema {
	if len(values) == 0 {
		panic("validate: String.OneOf: no values")
	}
	return s.with(strOp{kind: opOneOf, set: append([]string(nil), values...)})
}

// Default supplies a value when the input is absent. It does not apply to
// empty strings or null. The default still passes through normalisers and
// constraints.
func (s StringSchema) Default(v string) StringSchema { s.def = &v; return s }

// Refine adds a custom check, run after all constraints.
func (s StringSchema) Refine(fn func(string) error) StringSchema {
	r := make([]func(string) error, len(s.refines), len(s.refines)+1)
	copy(r, s.refines)
	s.refines = append(r, fn)
	return s
}

func mustNonNeg(what string, n int) {
	if n < 0 {
		panic(fmt.Sprintf("validate: %s: negative size %d", what, n))
	}
}

func (s StringSchema) run(c *pctx, in input) (string, bool) {
	var v string
	switch {
	case !in.present:
		if s.def == nil {
			c.required(in)
			return "", false
		}
		v = *s.def
	default:
		str, ok := in.v.(string)
		if !ok {
			c.typeErr(in, "string")
			return "", false
		}
		v = str
	}
	for _, o := range s.ops {
		switch o.kind {
		case opTrim:
			v = strings.TrimSpace(v)
		case opLower:
			v = strings.ToLower(v)
		case opUpper:
			v = strings.ToUpper(v)
		}
	}
	ok := true
	for _, o := range s.ops {
		if o.normalizer() {
			continue
		}
		if !o.check(c, in, v) {
			ok = false
		}
	}
	if !ok {
		return "", false
	}
	for _, r := range s.refines {
		if !applyRefine(c, in, v, r) {
			return "", false
		}
	}
	return v, true
}

func (o strOp) check(c *pctx, in input, v string) bool {
	switch o.kind {
	case opMin:
		if utf8.RuneCountInString(v) < o.n {
			c.fail(in, CodeTooSmall, fmt.Sprintf("must be at least %d characters", o.n), meta("minimum", o.n))
			return false
		}
	case opMax:
		if utf8.RuneCountInString(v) > o.n {
			c.fail(in, CodeTooLarge, fmt.Sprintf("must be at most %d characters", o.n), meta("maximum", o.n))
			return false
		}
	case opLength:
		if utf8.RuneCountInString(v) != o.n {
			c.fail(in, CodeInvalidLength, fmt.Sprintf("must be exactly %d characters", o.n), meta("length", o.n))
			return false
		}
	case opEmail:
		if !validEmail(v) {
			c.fail(in, CodeInvalidEmail, "must be a valid email address")
			return false
		}
	case opURL:
		if !validURL(v) {
			c.fail(in, CodeInvalidURL, "must be a valid absolute URL")
			return false
		}
	case opUUID:
		if !validUUID(v) {
			c.fail(in, CodeInvalidUUID, "must be a valid UUID")
			return false
		}
	case opRegex:
		if !o.re.MatchString(v) {
			c.fail(in, CodeInvalidFormat, "has an invalid format")
			return false
		}
	case opStartsWith:
		if !strings.HasPrefix(v, o.s) {
			c.fail(in, CodeInvalidString, fmt.Sprintf("must start with %q", o.s))
			return false
		}
	case opEndsWith:
		if !strings.HasSuffix(v, o.s) {
			c.fail(in, CodeInvalidString, fmt.Sprintf("must end with %q", o.s))
			return false
		}
	case opContains:
		if !strings.Contains(v, o.s) {
			c.fail(in, CodeInvalidString, fmt.Sprintf("must contain %q", o.s))
			return false
		}
	case opOneOf:
		for _, x := range o.set {
			if x == v {
				return true
			}
		}
		c.fail(in, CodeInvalidValue, "must be one of: "+strings.Join(o.set, ", "), meta("allowed", o.set))
		return false
	}
	return true
}

func validEmail(s string) bool {
	if len(s) == 0 || len(s) > 254 || strings.ContainsAny(s, " \t\r\n<>\"") {
		return false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s {
		return false
	}
	at := strings.LastIndexByte(s, '@')
	if at < 1 || at > 64 {
		return false
	}
	domain := s[at+1:]
	if !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

func validURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, err := uuid.Parse(s)
	return err == nil
}
