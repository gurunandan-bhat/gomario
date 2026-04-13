package service

import (
	"errors"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// handle wraps a handler function for HTML routes. If the handler returns an
// HTTPError the appropriate 4xx or 5xx template is rendered; any other error
// is treated as an unexpected 500.
func (s *Service) handle(fn func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.handleHTTPError(w, r, err)
		}
	})
}

// handleHTTPError resolves the HTTP status from err, records it on the OTel
// span, logs unexpected errors, and renders the appropriate error template.
func (s *Service) handleHTTPError(w http.ResponseWriter, r *http.Request, err error) {
	span := trace.SpanFromContext(r.Context())
	span.RecordError(err)

	var httpErr HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Code >= 500 {
			s.Logger.Error("internal error", "error", err, "path", r.URL.Path)
			span.SetStatus(codes.Error, err.Error())
		}
		s.renderErrorPage(w, httpErr.Code, httpErr.Message)
		return
	}

	// Unexpected error — always 500.
	s.Logger.Error("unexpected error", "error", err, "path", r.URL.Path)
	span.SetStatus(codes.Error, err.Error())
	s.renderErrorPage(w, http.StatusInternalServerError, "An unexpected error occurred. Please try again later.")
}

// renderErrorPage picks the right error template based on status and renders it.
// Falls back to plain text if the template itself fails.
func (s *Service) renderErrorPage(w http.ResponseWriter, status int, message string) {
	tmplName := "5xx.go.html"
	if status < 500 {
		tmplName = "4xx.go.html"
	}

	data := struct {
		Title   string
		Message string
	}{
		Title:   http.StatusText(status),
		Message: message,
	}

	if err := s.render(w, tmplName, data, nil, status); err != nil {
		s.Logger.Error("failed to render error template", "template", tmplName, "error", err)
		http.Error(w, message, status)
	}
}
