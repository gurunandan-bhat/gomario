package service

import (
	"net/http"

	"github.com/justinas/nosurf"
)

type Middleware func(serviceHandler) serviceHandler

func (s *Service) requireAuth(next serviceHandler) serviceHandler {

	return func(w http.ResponseWriter, r *http.Request) error {

		if !s.SessionManager.GetBool(r.Context(), "isAuthenticated") {
			s.SessionManager.Put(r.Context(), "redirectAfterLogin", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return nil
		}

		w.Header().Add("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
		return nil
	}
}

// requireGroup wraps a handler so it is only reachable by users who belong to
// the given Cognito group. Users who are authenticated but lack the group get
// a 403; unauthenticated users are redirected to /login first.
func (s *Service) requireGroup(group string) Middleware {
	return func(next serviceHandler) serviceHandler {
		authed := s.requireAuth(func(w http.ResponseWriter, r *http.Request) error {
			groups := s.SessionManager.Get(r.Context(), "userGroups")
			if gs, ok := groups.([]string); ok {
				for _, g := range gs {
					if g == group {
						return next(w, r)
					}
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
			return nil
		})
		return authed
	}
}

// apiAuthMiddleware is a standard chi-compatible middleware (func(http.Handler)
// http.Handler) for use with r.Use() on the /api/ sub-router. On missing or
// invalid sessions it returns a JSON 401 instead of redirecting.
func (s *Service) apiAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.SessionManager.GetBool(r.Context(), "isAuthenticated") {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"unauthorized"}`)) //nolint:errcheck
			return
		}
		w.Header().Add("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// apiRequireGroup returns a chi-compatible middleware that allows only users
// belonging to the given Cognito group. Unauthenticated requests get a JSON
// 401; authenticated requests without the group get a JSON 403.
func (s *Service) apiRequireGroup(group string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.apiAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			groups := s.SessionManager.Get(r.Context(), "userGroups")
			if gs, ok := groups.([]string); ok {
				for _, g := range gs {
					if g == group {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"forbidden"}`)) //nolint:errcheck
		}))
	}
}

// Create a NoSurf middleware function which uses a customized CSRF cookie with
// the Secure, Path and HttpOnly attributes set.
func noSurf(next http.Handler) http.Handler {
	csrfHandler := nosurf.New(next)
	csrfHandler.SetBaseCookie(http.Cookie{
		HttpOnly: true,
		Path:     "/",
		Secure:   true,
	})
	csrfHandler.ExemptPaths([]string{}...)

	return csrfHandler
}
