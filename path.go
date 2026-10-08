package validate

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Segment is one step of a Path: an object field name, a map key or an array
// index.
type Segment struct {
	Key     string
	Index   int
	IsIndex bool
}

// String renders a segment without surrounding punctuation.
func (s Segment) String() string {
	if s.IsIndex {
		return strconv.Itoa(s.Index)
	}
	return s.Key
}

// MarshalJSON renders keys as JSON strings and indexes as JSON numbers.
func (s Segment) MarshalJSON() ([]byte, error) {
	if s.IsIndex {
		return json.Marshal(s.Index)
	}
	return json.Marshal(s.Key)
}

// UnmarshalJSON is the inverse of MarshalJSON.
func (s *Segment) UnmarshalJSON(b []byte) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return err
	}
	switch x := v.(type) {
	case string:
		*s = Segment{Key: x}
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return err
		}
		*s = Segment{Index: int(n), IsIndex: true}
	default:
		return fmt.Errorf("validate: invalid path segment %s", b)
	}
	return nil
}

// Path locates a value inside the validated input. Its JSON form is an array
// such as ["items", 3, "quantity"].
type Path []Segment

// Key returns a field-name or map-key segment.
func Key(k string) Segment { return Segment{Key: k} }

// Idx returns an array-index segment.
func Idx(i int) Segment { return Segment{Index: i, IsIndex: true} }

// PathOf builds a Path from strings (keys) and ints (indexes). It panics on
// any other type, since that is a programming error.
func PathOf(parts ...any) Path {
	p := make(Path, 0, len(parts))
	for _, part := range parts {
		switch x := part.(type) {
		case string:
			p = append(p, Key(x))
		case int:
			p = append(p, Idx(x))
		case Segment:
			p = append(p, x)
		default:
			panic(fmt.Sprintf("validate: PathOf: unsupported segment type %T", part))
		}
	}
	return p
}

// String renders the path for humans, e.g. items[1].quantity. The structured
// form is the contract; this is for logs.
func (p Path) String() string {
	var b strings.Builder
	for i, s := range p {
		switch {
		case s.IsIndex:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.Index))
			b.WriteByte(']')
		case i == 0:
			b.WriteString(s.Key)
		default:
			b.WriteByte('.')
			b.WriteString(s.Key)
		}
	}
	return b.String()
}

// Equal reports whether two paths are identical.
func (p Path) Equal(o Path) bool {
	if len(p) != len(o) {
		return false
	}
	for i := range p {
		if p[i] != o[i] {
			return false
		}
	}
	return true
}

// with returns a copy of p extended by segs. It never aliases p's backing
// array, so sibling paths cannot overwrite each other.
func (p Path) with(segs ...Segment) Path {
	out := make(Path, len(p), len(p)+len(segs))
	copy(out, p)
	return append(out, segs...)
}
