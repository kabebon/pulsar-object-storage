// Package api implements the REST API v1 (Bearer-token authenticated) on top
// of the service layer. Responses are JSON and errors follow RFC 9457.
package api

import (
	"encoding/json"
	"net/http"

	httperr "pulsar/internal/errors"
)

// writeJSON marshals v as application/json.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError serializes an AppError as RFC 9457 application/problem+json.
func writeError(w http.ResponseWriter, e *httperr.AppError) {
	if e == nil {
		e = httperr.Internal(nil)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "https://pulsar.local/errors/" + e.Code,
		"title":  e.Title,
		"status": e.Status,
		"code":   e.Code,
		"detail": e.Detail,
		"fields": e.Fields,
	})
}

// decode reads a JSON body into dst. Returns an AppError on failure.
func decode(r *http.Request, dst any) *httperr.AppError {
	if r.Body == nil {
		return httperr.BadRequest("bad_body", "Request body is required")
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return httperr.BadRequest("bad_json", "Invalid JSON body: "+err.Error())
	}
	return nil
}

// ok returns a minimal success envelope.
type ok struct {
	OK bool `json:"ok"`
}
