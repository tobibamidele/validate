package validate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes is the request body limit JSON applies unless
// WithMaxBodyBytes says otherwise: 1 MiB.
const DefaultMaxBodyBytes int64 = 1 << 20

// RequestError is a transport-level failure that is not a schema violation:
// an oversized body or a wrong Content-Type. WriteError maps it to Status.
type RequestError struct {
	Status  int
	Code    string
	Message string
}

func (e *RequestError) Error() string { return e.Code + ": " + e.Message }

// HTTPOption configures the HTTP helpers.
type HTTPOption func(*httpConfig)

type httpConfig struct {
	maxBody     int64
	contentType ctMode
	allowEmpty  bool
	parse       []Option
}

type ctMode uint8

const (
	ctIfPresent ctMode = iota // default: if Content-Type is sent it must be JSON
	ctRequire                 // must be sent and be JSON
	ctIgnore
)

func newHTTPConfig(opts []HTTPOption) httpConfig {
	cfg := httpConfig{maxBody: DefaultMaxBodyBytes}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// WithMaxBodyBytes sets the JSON body limit. A value <= 0 disables the limit.
func WithMaxBodyBytes(n int64) HTTPOption { return func(c *httpConfig) { c.maxBody = n } }

// RequireJSONContentType makes a missing Content-Type an error too.
func RequireJSONContentType() HTTPOption { return func(c *httpConfig) { c.contentType = ctRequire } }

// IgnoreContentType skips Content-Type checking.
func IgnoreContentType() HTTPOption { return func(c *httpConfig) { c.contentType = ctIgnore } }

// AllowEmptyBody treats an empty body as {} instead of an error.
func AllowEmptyBody() HTTPOption { return func(c *httpConfig) { c.allowEmpty = true } }

// WithParseOptions forwards per-parse Options (WithAbortEarly, WithValues).
func WithParseOptions(o ...Option) HTTPOption {
	return func(c *httpConfig) { c.parse = append(c.parse, o...) }
}

// JSON reads, decodes and validates a JSON request body.
//
// The body is decoded to a generic value (numbers kept exact as json.Number)
// and handed to the schema, so absent and null stay distinguishable. It
// rejects empty bodies, malformed JSON and trailing data. Failures return a
// *ValidationError (invalid_json, required, ...) or a *RequestError
// (413 too large, 415 wrong content type).
func JSON[T any](r *http.Request, schema Schema[T], opts ...HTTPOption) (T, error) {
	var zero T
	cfg := newHTTPConfig(opts)
	if err := checkContentType(r, cfg.contentType); err != nil {
		return zero, err
	}
	var body io.Reader = r.Body
	if r.Body == nil {
		body = http.NoBody
	}
	if cfg.maxBody > 0 {
		body = io.LimitReader(body, cfg.maxBody+1)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return zero, &RequestError{Status: http.StatusBadRequest, Code: "read_error", Message: "could not read request body"}
	}
	if cfg.maxBody > 0 && int64(len(data)) > cfg.maxBody {
		return zero, &RequestError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body is too large"}
	}
	return fromJSON(data, schema, cfg.allowEmpty, cfg.parse)
}

// FromJSON validates JSON bytes against a schema, with the same strictness as
// JSON. Use it for message payloads that do not come from an HTTP request.
func FromJSON[T any](data []byte, schema Schema[T], opts ...Option) (T, error) {
	return fromJSON(data, schema, false, opts)
}

func fromJSON[T any](data []byte, schema Schema[T], allowEmpty bool, opts []Option) (T, error) {
	var zero T
	if len(bytes.TrimSpace(data)) == 0 {
		if allowEmpty {
			return ParseContext(backgroundCtx(), schema, map[string]any{}, opts...)
		}
		return zero, &ValidationError{Issues: []Issue{{Path: Path{}, Code: CodeRequired, Message: "request body is required"}}}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return zero, invalidJSON("request body is not valid JSON")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return zero, invalidJSON("request body contains data after the JSON value")
	}
	return ParseContext(backgroundCtx(), schema, v, opts...)
}

func invalidJSON(msg string) *ValidationError {
	return &ValidationError{Issues: []Issue{{Path: Path{}, Code: CodeInvalidJSON, Message: msg}}}
}

func checkContentType(r *http.Request, mode ctMode) error {
	if mode == ctIgnore {
		return nil
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		if mode == ctRequire {
			return &RequestError{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type", Message: "Content-Type must be application/json"}
		}
		return nil
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json")) {
		return nil
	}
	return &RequestError{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type", Message: "Content-Type must be application/json"}
}
