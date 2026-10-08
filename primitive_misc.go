package validate

import (
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// BytesSchema validates []byte. Output is always a fresh copy, so it never
// aliases caller-owned input. Min/Max/Length count bytes.
type BytesSchema struct {
	b64       bool
	min, max  int
	length    int
	hasMin    bool
	hasMax    bool
	hasLength bool
}

// Bytes returns a schema for []byte.
func Bytes() BytesSchema { return BytesSchema{} }

// Base64 additionally accepts a standard-encoding base64 string, which is how
// bytes travel in JSON.
func (s BytesSchema) Base64() BytesSchema { s.b64 = true; return s }

// Min requires at least n bytes.
func (s BytesSchema) Min(n int) BytesSchema {
	mustNonNeg("Bytes.Min", n)
	s.min, s.hasMin = n, true
	return s
}

// Max allows at most n bytes.
func (s BytesSchema) Max(n int) BytesSchema {
	mustNonNeg("Bytes.Max", n)
	s.max, s.hasMax = n, true
	return s
}

// Length requires exactly n bytes.
func (s BytesSchema) Length(n int) BytesSchema {
	mustNonNeg("Bytes.Length", n)
	s.length, s.hasLength = n, true
	return s
}

func (s BytesSchema) run(c *pctx, in input) ([]byte, bool) {
	if !in.present {
		c.required(in)
		return nil, false
	}
	var out []byte
	switch x := in.v.(type) {
	case []byte:
		out = append([]byte(nil), x...)
	case string:
		if !s.b64 {
			c.typeErr(in, "bytes")
			return nil, false
		}
		d, err := base64.StdEncoding.DecodeString(x)
		if err != nil {
			c.fail(in, CodeInvalidBytes, "must be valid base64")
			return nil, false
		}
		out = d
	default:
		c.typeErr(in, "bytes")
		return nil, false
	}
	ok := true
	if s.hasMin && len(out) < s.min {
		c.fail(in, CodeTooSmall, fmt.Sprintf("must be at least %d bytes", s.min), meta("minimum", s.min))
		ok = false
	}
	if s.hasMax && len(out) > s.max {
		c.fail(in, CodeTooLarge, fmt.Sprintf("must be at most %d bytes", s.max), meta("maximum", s.max))
		ok = false
	}
	if s.hasLength && len(out) != s.length {
		c.fail(in, CodeInvalidLength, fmt.Sprintf("must be exactly %d bytes", s.length), meta("length", s.length))
		ok = false
	}
	return out, ok
}

// TimeSchema validates time.Time. It never silently parses strings; call
// Layout to opt in to a specific text format, or use Transform.
type TimeSchema struct {
	layout        string
	before, after *time.Time
	notBefore     *time.Time
	notAfter      *time.Time
}

// Time returns a schema for time.Time.
func Time() TimeSchema { return TimeSchema{} }

// Layout additionally accepts strings in the given time.Parse layout.
func (s TimeSchema) Layout(layout string) TimeSchema { s.layout = layout; return s }

// Before requires the value to be strictly before t.
func (s TimeSchema) Before(t time.Time) TimeSchema { s.before = &t; return s }

// After requires the value to be strictly after t.
func (s TimeSchema) After(t time.Time) TimeSchema { s.after = &t; return s }

// Between requires start <= value <= end (inclusive).
func (s TimeSchema) Between(start, end time.Time) TimeSchema {
	if end.Before(start) {
		panic("validate: Time.Between: end is before start")
	}
	s.notBefore, s.notAfter = &start, &end
	return s
}

func (s TimeSchema) run(c *pctx, in input) (time.Time, bool) {
	var zero time.Time
	if !in.present {
		c.required(in)
		return zero, false
	}
	var t time.Time
	switch x := in.v.(type) {
	case time.Time:
		t = x
	case string:
		if s.layout == "" {
			c.typeErr(in, "time")
			return zero, false
		}
		p, err := time.Parse(s.layout, x)
		if err != nil {
			c.fail(in, CodeInvalidDate, "must be a valid date/time")
			return zero, false
		}
		t = p
	default:
		c.typeErr(in, "time")
		return zero, false
	}
	ok := true
	if s.before != nil && !t.Before(*s.before) {
		c.fail(in, CodeTooLarge, "must be before "+s.before.Format(time.RFC3339), meta("before", s.before.Format(time.RFC3339)))
		ok = false
	}
	if s.after != nil && !t.After(*s.after) {
		c.fail(in, CodeTooSmall, "must be after "+s.after.Format(time.RFC3339), meta("after", s.after.Format(time.RFC3339)))
		ok = false
	}
	if s.notBefore != nil && t.Before(*s.notBefore) {
		c.fail(in, CodeTooSmall, "must not be before "+s.notBefore.Format(time.RFC3339))
		ok = false
	}
	if s.notAfter != nil && t.After(*s.notAfter) {
		c.fail(in, CodeTooLarge, "must not be after "+s.notAfter.Format(time.RFC3339))
		ok = false
	}
	if !ok {
		return zero, false
	}
	return t, true
}

// UUIDSchema validates UUIDs and produces uuid.UUID. It accepts a uuid.UUID or
// the canonical 36-character text form.
type UUIDSchema struct{}

// UUID returns a schema producing uuid.UUID.
func UUID() UUIDSchema { return UUIDSchema{} }

func (UUIDSchema) run(c *pctx, in input) (uuid.UUID, bool) {
	if !in.present {
		c.required(in)
		return uuid.UUID{}, false
	}
	switch x := in.v.(type) {
	case uuid.UUID:
		return x, true
	case string:
		if validUUID(x) {
			u, _ := uuid.Parse(x)
			return u, true
		}
		c.fail(in, CodeInvalidUUID, "must be a valid UUID")
		return uuid.UUID{}, false
	}
	c.typeErr(in, "uuid")
	return uuid.UUID{}, false
}
