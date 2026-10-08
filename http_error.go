package validate

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the framework-neutral JSON shape of a validation failure.
// Issue.Value is never serialised.
type ErrorResponse struct {
	Error   string  `json:"error"`
	Message string  `json:"message"`
	Issues  []Issue `json:"issues"`
}

// Response converts a validation error to its wire shape.
func (e *ValidationError) Response() ErrorResponse {
	issues := e.Issues
	if issues == nil {
		issues = []Issue{}
	}
	return ErrorResponse{Error: "validation_error", Message: "request validation failed", Issues: issues}
}

// WriteOptions customises WriteErrorWith.
type WriteOptions struct {
	// ValidationStatus defaults to 400.
	ValidationStatus int
	// Format replaces the default body for validation errors.
	Format func(*ValidationError) any
}

// WriteError writes err as JSON. A *ValidationError becomes a 400 with its
// structured issues; a *RequestError uses its own status; anything else is a
// generic 500 that does not expose the error text.
func WriteError(w http.ResponseWriter, err error) { WriteErrorWith(w, err, WriteOptions{}) }

// WriteErrorWith is WriteError with configurable status and body.
func WriteErrorWith(w http.ResponseWriter, err error, o WriteOptions) {
	status, body := http.StatusInternalServerError, any(map[string]string{
		"error": "internal_error", "message": "internal server error",
	})
	if ve, ok := AsValidationError(err); ok {
		status = o.ValidationStatus
		if status == 0 {
			status = http.StatusBadRequest
		}
		if o.Format != nil {
			body = o.Format(ve)
		} else {
			body = ve.Response()
		}
	} else if re, ok := asRequestError(err); ok {
		status = re.Status
		body = map[string]string{"error": re.Code, "message": re.Message}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
