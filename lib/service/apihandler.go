package service

import (
	"encoding/json"
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// handleAPI wraps a handler function for JSON API routes. Errors are resolved
// to JSON responses; HTTPError controls the status code, anything else is 500.
func (s *Service) handleAPI(fn func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.handleAPIError(w, r, err)
		}
	})
}

// handleAPIError resolves the HTTP status from err, records it on the OTel
// span, logs unexpected errors, and writes a JSON error body.
func (s *Service) handleAPIError(w http.ResponseWriter, r *http.Request, err error) {
	span := trace.SpanFromContext(r.Context())
	span.RecordError(err)

	status := http.StatusInternalServerError
	message := "an unexpected error occurred"

	var httpErr HTTPError
	if errors.As(err, &httpErr) {
		status = httpErr.Code
		message = httpErr.Message
		if status >= 500 {
			s.Logger.Error("internal api error", "error", err, "path", r.URL.Path)
			span.SetStatus(codes.Error, err.Error())
		}
	} else {
		s.Logger.Error("unexpected api error", "error", err, "path", r.URL.Path)
		span.SetStatus(codes.Error, err.Error())
	}

	body, _ := json.Marshal(map[string]string{"message": message})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body) //nolint:errcheck
}
