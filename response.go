package validate

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

// ResponseError reports that a value the server was about to send does not
// satisfy its response schema. That is a bug in the server, not bad client
// input, so it is deliberately not a *ValidationError: WriteError turns it
// into a generic 500 and never exposes its issues. Log it.
type ResponseError struct {
	Issues []Issue
	cause  error
}

// Error summarises the contract violation for logs.
func (e *ResponseError) Error() string {
	ve := ValidationError{Issues: e.Issues}
	msg := ve.Error()
	if e.cause != nil && len(e.Issues) == 0 {
		return "response validation failed: " + e.cause.Error()
	}
	return "response " + msg
}

// Unwrap exposes the encoding error, if the value could not be marshalled.
func (e *ResponseError) Unwrap() error { return e.cause }

// AsResponseError extracts a *ResponseError from err.
func AsResponseError(err error) (*ResponseError, bool) {
	var re *ResponseError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}

// JSONResponse checks that value, once encoded as JSON, satisfies schema. It
// uses the same engine and semantics as request parsing, which makes the
// schema one contract for both directions.
//
// Things to know:
//
//   - value is encoded with encoding/json, so your json tags must produce the
//     schema's field names, exactly as the client will see them.
//   - Normalisers (Trim, Lower, Upper) and Coerce are part of the schema, so
//     they apply here too: JSONResponse proves the value would be accepted,
//     not that it is already normalised. Put such rules on request schemas.
//   - time.Time encodes to a string, so a response schema needs
//     Time().Layout(time.RFC3339Nano); []byte encodes to base64, so use
//     Bytes().Base64().
//   - It returns only an error; the decoded value is discarded.
func JSONResponse[T any](schema Schema[T], value any, opts ...Option) error {
	_, err := encodeAndCheck(schema, value, opts)
	return err
}

func encodeAndCheck[T any](schema Schema[T], value any, opts []Option) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(value); err != nil {
		return nil, &ResponseError{cause: err}
	}
	if _, err := FromJSON(buf.Bytes(), schema, opts...); err != nil {
		if ve, ok := AsValidationError(err); ok {
			return nil, &ResponseError{Issues: ve.Issues}
		}
		return nil, &ResponseError{cause: err}
	}
	return buf.Bytes(), nil
}

// RespondJSON validates value against schema, then writes it with the given
// status. The bytes that were validated are exactly the bytes sent. If the
// value violates the schema, nothing of it is written: the client gets a
// generic 500 and the *ResponseError is returned for you to log.
func RespondJSON[T any](w http.ResponseWriter, status int, schema Schema[T], value any, opts ...Option) error {
	body, err := encodeAndCheck(schema, value, opts)
	if err != nil {
		WriteError(w, err)
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, werr := w.Write(body)
	return werr
}
