package service

import (
	"net/http"
)

// healthz is the liveness probe. It always returns 200 — if the process is
// running and able to serve HTTP, it is alive.
func (s *Service) healthz(w http.ResponseWriter, r *http.Request) error {
	return s.renderJSON(w, map[string]string{"status": "ok"}, http.StatusOK)
}

// readyz is the readiness probe. It pings the database; if the ping fails the
// service is not ready to accept traffic and returns 503.
func (s *Service) readyz(w http.ResponseWriter, r *http.Request) error {
	if err := s.Model.DbHandle.PingContext(r.Context()); err != nil {
		s.Logger.Error("readyz: db ping failed", "error", err)
		return s.renderJSON(w, map[string]string{"status": "unavailable"}, http.StatusServiceUnavailable)
	}
	return s.renderJSON(w, map[string]string{"status": "ok"}, http.StatusOK)
}
