package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"officerdashboard/internal/files"
	"officerdashboard/internal/store"
	"officerdashboard/internal/validate"
)

// APIError is the ApiError body: {error: {code, message, details?}} plus its HTTP status.
type APIError struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

func fail(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

func badRequest(message string) *APIError { return fail(400, "bad_request", message) }
func conflict(message string) *APIError   { return fail(409, "conflict", message) }
func forbidden(message string) *APIError  { return fail(403, "forbidden", message) }
func notFoundErr(message string) *APIError {
	return fail(404, "not_found", message)
}

// validationError is the 400 for a failed schema check, with zod-style flatten() details.
func validationError(i *validate.Issues) *APIError {
	return &APIError{Status: 400, Code: "bad_request", Message: "Validation failed", Details: i.Flatten()}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, e *APIError) {
	body := map[string]any{"code": e.Code, "message": e.Message}
	if e.Details != nil {
		body["details"] = e.Details
	}
	writeJSON(w, e.Status, map[string]any{"error": body})
}

var (
	conflictRE = regexp.MustCompile(`(?i)constraint|mutation_guard|last active director|closed|open period|immutable|UNIQUE`)
	lastDirRE  = regexp.MustCompile(`last active director`)
)

// toAPIError maps any error from a handler to the response v1 gave: SQLite constraint and
// trigger failures become 409 conflicts, upload problems 400, anything else a logged 500.
func toAPIError(err error, log *slog.Logger) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	switch {
	case errors.Is(err, context.Canceled):
		// The client hung up; nobody is reading the answer. Not a server fault, not logged as one.
		log.Debug("request canceled by the client")
		return fail(499, "internal", "Request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		log.Debug("request timed out")
		return fail(504, "internal", "Request timed out")
	case errors.Is(err, files.ErrUnavailable):
		return fail(503, "internal", err.Error())
	case errors.Is(err, files.ErrInvalidUpload):
		return badRequest(err.Error())
	case errors.Is(err, store.ErrNotFound):
		return notFoundErr("Record not found")
	}
	msg := err.Error()
	if conflictRE.MatchString(msg) {
		log.Warn("database conflict mapped to 409", "error", msg)
		if lastDirRE.MatchString(msg) {
			return conflict("Cannot remove the last active director")
		}
		return conflict("Conflicting state, closed period, or duplicate record")
	}
	log.Error("API operation failed", "error", msg)
	return fail(500, "internal", "Operation failed")
}
