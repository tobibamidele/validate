package validate

import (
	"context"
	"net/http"
	"strings"
)

func backgroundCtx() context.Context { return context.Background() }

// shapesOf returns the declared fields of an object schema. Transport values
// are strings, so adapters need to know which fields are arrays: repeated
// keys (and a single occurrence of an array field) become []any, scalar
// fields get the single string, and a scalar field that was repeated receives
// the whole list so its schema rejects it with invalid_type.
func shapesOf[T any](schema Schema[T]) []fieldShape {
	if o, ok := any(schema).(objectShaper); ok {
		return o.objectFields()
	}
	panic("validate: query/path/header/cookie helpers need an Object schema")
}

func buildKV(shapes []fieldShape, lookup func(name string) []string) map[string]any {
	m := make(map[string]any, len(shapes))
	for _, f := range shapes {
		vals := lookup(f.name)
		if len(vals) == 0 {
			continue
		}
		switch {
		case f.array || len(vals) > 1:
			arr := make([]any, len(vals))
			for i, v := range vals {
				arr[i] = v
			}
			m[f.name] = arr
		default:
			m[f.name] = vals[0]
		}
	}
	return m
}

func kvParse[T any](schema Schema[T], lookup func(string) []string, opts []HTTPOption) (T, error) {
	cfg := newHTTPConfig(opts)
	return ParseContext(backgroundCtx(), schema, buildKV(shapesOf(schema), lookup), cfg.parse...)
}

// Query validates URL query parameters against an Object schema. Query
// strings are text, so numeric and boolean fields need explicit Coerce().
// Repeated keys become arrays for array fields; a single occurrence of an
// array field still yields a one-element array.
func Query[T any](r *http.Request, schema Schema[T], opts ...HTTPOption) (T, error) {
	q := r.URL.Query()
	return kvParse(schema, func(n string) []string { return q[n] }, opts)
}

// PathValues validates named route parameters, extracted by your router into
// a map, against an Object schema.
func PathValues[T any](values map[string]string, schema Schema[T], opts ...HTTPOption) (T, error) {
	return kvParse(schema, func(n string) []string {
		if v, ok := values[n]; ok {
			return []string{v}
		}
		return nil
	}, opts)
}

// Headers validates request headers against an Object schema. Lookup is
// case-insensitive; a field named "user_agent" also matches User-Agent.
// Repeated headers become arrays for array fields.
func Headers[T any](h http.Header, schema Schema[T], opts ...HTTPOption) (T, error) {
	return kvParse(schema, func(n string) []string {
		if v := h.Values(n); len(v) > 0 {
			return v
		}
		return h.Values(strings.ReplaceAll(n, "_", "-"))
	}, opts)
}

// Cookies validates request cookies (text values) against an Object schema.
func Cookies[T any](r *http.Request, schema Schema[T], opts ...HTTPOption) (T, error) {
	return kvParse(schema, func(n string) []string {
		var vals []string
		for _, c := range r.Cookies() {
			if c.Name == n {
				vals = append(vals, c.Value)
			}
		}
		return vals
	}, opts)
}
