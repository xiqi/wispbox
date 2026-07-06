// Package httpjson holds the small JSON request/response helpers shared by
// every API package.
package httpjson

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/xiqi/wispbox/internal/db"
)

// Write sends a JSON response.
func Write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		v = map[string]any{"ok": true}
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("encode response", "error", err)
	}
}

// Decode reads a JSON body with a size cap.
func Decode(w http.ResponseWriter, r *http.Request, v any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("request body is not valid JSON for this endpoint")
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

// Error sends a human-readable error with exactly the status given. Callers
// that want error-kind-based status mapping (e.g. not-found → 404) should use
// Fail instead.
func Error(w http.ResponseWriter, status int, err error) {
	msg := "something went wrong"
	if err != nil {
		msg = err.Error()
	}
	Write(w, status, map[string]string{"error": msg})
}

// Fail maps common error kinds to statuses: validation errors are 400,
// missing rows are 404, the rest are 500.
func Fail(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		Write(w, http.StatusOK, nil)
	case errors.Is(err, db.ErrNotFound):
		Error(w, http.StatusNotFound, err)
	default:
		Error(w, http.StatusBadRequest, err)
	}
}

// ClientIP extracts the peer address (wispbox terminates TLS itself; there
// is no trusted proxy in front, so X-Forwarded-For is ignored on purpose).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
