package api

import (
	"log/slog"
	"net/http"
	"time"
)

// statusWriter records the status a handler sends. It only observes WriteHeader: response
// bytes go straight to the underlying writer (the embedded Write), never through this code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// AccessLog logs one line per request (health checks excluded). Query strings are left out
// of the log because they can carry OAuth codes.
func AccessLog(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK // the handler wrote a body without calling WriteHeader
		}
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "ms", float64(time.Since(start).Microseconds())/1000)
	})
}
