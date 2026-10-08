package validate

import (
	"errors"
	"fmt"
	"strings"
)

// Built-in issue codes. Codes are stable and machine readable; messages are
// not a wire contract.
const (
	CodeRequired       = "required"
	CodeInvalidType    = "invalid_type"
	CodeInvalidJSON    = "invalid_json"
	CodeInvalidString  = "invalid_string"
	CodeInvalidEmail   = "invalid_email"
	CodeInvalidURL     = "invalid_url"
	CodeInvalidUUID    = "invalid_uuid"
	CodeInvalidRegex   = "invalid_regex"
	CodeInvalidFormat  = "invalid_format"
	CodeInvalidValue   = "invalid_value"
	CodeNotEmpty       = "not_empty"
	CodeTooSmall       = "too_small"
	CodeTooLarge       = "too_large"
	CodeInvalidLength  = "invalid_length"
	CodeNotMultipleOf  = "not_multiple_of"
	CodeInvalidEnum    = "invalid_enum"
	CodeInvalidUnion   = "invalid_union"
	CodeInvalidArray   = "invalid_array"
	CodeInvalidObject  = "invalid_object"
	CodeUnknownKey     = "unknown_key"
	CodeInvalidDate    = "invalid_date"
	CodeInvalidTime    = "invalid_time"
	CodeInvalidBytes   = "invalid_bytes"
	CodeTooDeep        = "too_deep"
	CodeTransformError = "transform_error"
	CodeCustom         = "custom"
)

// Issue is one validation failure.
type Issue struct {
	Path     Path           `json:"path"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Expected string         `json:"expected,omitempty"`
	Received string         `json:"received,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`

	// Value is the offending raw input. It is only populated when the caller
	// passes WithValues, because inputs may be passwords or tokens. It is never
	// serialised.
	Value any `json:"-"`

	cause error
}

// Cause returns the underlying error for transform/refinement failures, for
// logging. It must not be shown to API clients.
func (i Issue) Cause() error { return i.cause }

// ValidationError is the error returned when input does not satisfy a schema.
type ValidationError struct {
	Issues []Issue
}

const maxErrorStringIssues = 10

// Error summarises the failure for logs, e.g.
// "validation failed: email: invalid_email; age: too_small". It never includes
// input values. The prose is not a wire contract.
func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "validation failed"
	}
	var b strings.Builder
	b.WriteString("validation failed: ")
	for i, is := range e.Issues {
		if i == maxErrorStringIssues {
			fmt.Fprintf(&b, "; (+%d more)", len(e.Issues)-i)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		p := is.Path.String()
		if p == "" {
			p = "(root)"
		}
		b.WriteString(p)
		b.WriteString(": ")
		b.WriteString(is.Code)
	}
	return b.String()
}

// Has reports whether any issue carries the given code.
func (e *ValidationError) Has(code string) bool {
	for _, is := range e.Issues {
		if is.Code == code {
			return true
		}
	}
	return false
}

// Field returns the issues located exactly at the given path. Index segments
// are matched by their decimal form, so Field("items", "1", "quantity") finds
// items[1].quantity.
func (e *ValidationError) Field(path ...string) []Issue {
	var out []Issue
	for _, is := range e.Issues {
		if len(is.Path) != len(path) {
			continue
		}
		match := true
		for i, seg := range is.Path {
			if seg.String() != path[i] {
				match = false
				break
			}
		}
		if match {
			out = append(out, is)
		}
	}
	return out
}

// IsValidationError reports whether err is (or wraps) a *ValidationError.
func IsValidationError(err error) bool {
	_, ok := AsValidationError(err)
	return ok
}

// AsValidationError extracts a *ValidationError from err.
func AsValidationError(err error) (*ValidationError, bool) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

// RefineError is the error type custom refinements and transforms return to
// control the resulting Issue. Create one with Error or IssueAt.
type RefineError struct {
	Code     string
	Message  string
	Path     Path // relative to the value being refined
	Metadata map[string]any
}

// Error implements error.
func (e *RefineError) Error() string { return e.Code + ": " + e.Message }

// WithMetadata attaches one metadata entry and returns the error.
func (e *RefineError) WithMetadata(key string, value any) *RefineError {
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	e.Metadata[key] = value
	return e
}

// Error creates a refinement failure with a custom code and message, reported
// at the refined value's own path.
func Error(code, message string) *RefineError {
	return &RefineError{Code: code, Message: message}
}

// IssueAt creates a refinement failure reported at a path relative to the
// refined value. Object-level refinements use it to point at a field.
func IssueAt(path Path, code, message string) *RefineError {
	return &RefineError{Code: code, Message: message, Path: path}
}

// Result is the outcome of SafeParse.
type Result[T any] struct {
	Value T
	Error *ValidationError
	OK    bool
}
