package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

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

var lastDirRE = regexp.MustCompile(`last active director`)

// errNoOpenPeriod is the accounting engine's own refusal (not a SQLite error) when an entry
// date falls outside every open period; it answers 409 like the trigger that guards the same rule.
const errNoOpenPeriod = "entry requires an open period"

// toAPIError maps any error from a handler to the response v1 gave: SQLite uniqueness, FK and
// trigger failures become 409 conflicts, upload problems 400, anything else (NOT NULL and
// CHECK failures included: those are handler bugs) a logged 500.
// Conflicts are recognised by the driver's error code (store.IsConflict), never by words
// such as "closed" in the text, so network and closed-database errors stay 500s.
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
	case errors.Is(err, files.ErrQuota), errors.Is(err, files.ErrRateLimited):
		// Drive is full or throttling us after retries: say so plainly (storeFile alerts).
		return fail(507, "storage_quota", "Google Drive storage is full or busy, so the file was not saved. Try again later; the owners have been alerted.")
	case errors.Is(err, files.ErrNotConnected), errors.Is(err, files.ErrReconnect):
		return fail(503, "not_configured", err.Error())
	case errors.Is(err, files.ErrUnavailable):
		return fail(503, "internal", err.Error())
	case errors.Is(err, files.ErrInvalidUpload):
		return badRequest(err.Error())
	case errors.Is(err, store.ErrNotFound):
		return notFoundErr("Record not found")
	}
	msg := err.Error()
	if strings.Contains(msg, errNoOpenPeriod) {
		log.Warn("database conflict mapped to 409", "error", msg)
		return conflict("Conflicting state, closed period, or duplicate record")
	}
	if !store.IsConflict(err) {
		log.Error("API operation failed", "error", msg)
		return fail(500, "internal", "Operation failed")
	}
	if strings.Contains(msg, "last owner") {
		return conflict("Cannot remove the last owner")
	}
	if strings.Contains(msg, "replace an owner") {
		return conflict("Cannot replace an owner")
	}
	if strings.Contains(msg, "last active leadership") {
		return conflict("Cannot remove the last active leadership user")
	}
	if strings.Contains(msg, "invites.discord_id") {
		return conflict("An invite for this Discord ID is already pending")
	}
	if strings.Contains(msg, "at most 5 calendar emails") {
		return conflict("You can save at most 5 calendar emails")
	}
	log.Warn("database conflict mapped to 409", "error", msg)
	if lastDirRE.MatchString(msg) {
		return conflict("Cannot remove the last active director")
	}
	return conflict("Conflicting state, closed period, or duplicate record")
}
