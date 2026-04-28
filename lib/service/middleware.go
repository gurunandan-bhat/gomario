package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/justinas/nosurf"
)

type contextKey string

const bearerClaimsKey contextKey = "bearerClaims"

type bearerClaims struct {
	Sub    string
	Groups []string
}

// requireAuth is a chi-compatible middleware for HTML routes. Unauthenticated
// requests are redirected to /login; the original URL is stored in the session
// so the user can be sent there after signing in.
func (s *Service) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.SessionManager.GetBool(r.Context(), "isAuthenticated") {
			s.SessionManager.Put(r.Context(), "redirectAfterLogin", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Add("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requireGroup returns a chi-compatible middleware that restricts access to
// users who belong to the given Cognito group. Unauthenticated users are
// redirected to /login first; authenticated users without the group get a
// rendered 403 page.
func (s *Service) requireGroup(group string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			groups := s.SessionManager.Get(r.Context(), "userGroups")
			if gs, ok := groups.([]string); ok {
				for _, g := range gs {
					if g == group {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			s.renderErrorPage(w, http.StatusForbidden, "You don't have permission to access this page.")
		}))
	}
}

// apiAuthMiddleware is a chi-compatible middleware for the /api/ sub-router.
// Unauthenticated requests receive a JSON 401 instead of a redirect.
func (s *Service) apiAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.SessionManager.GetBool(r.Context(), "isAuthenticated") {
			s.handleAPIError(w, r, ErrUnauthorized("unauthorized"))
			return
		}
		w.Header().Add("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// apiRequireGroup returns a chi-compatible middleware that restricts an API
// route to users in the given Cognito group. Unauthenticated → JSON 401;
// authenticated but wrong group → JSON 403.
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
			s.handleAPIError(w, r, ErrForbidden("forbidden"))
		}))
	}
}

// apiBearerAuthMiddleware validates the Bearer token in the Authorization
// header for /api routes called by the Vue SPA. Returns JSON 401 on failure.
func (s *Service) apiBearerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		if rawToken == "" || rawToken == authHeader {
			s.handleAPIError(w, r, ErrUnauthorized("unauthorized"))
			return
		}

		token, err := s.JWKSCache.validateAccessToken(r.Context(), rawToken, s.Config.Cognito.Region, s.Config.Cognito.UserPoolID)
		if err != nil {
			s.handleAPIError(w, r, ErrUnauthorized("unauthorized"))
			return
		}

		sub, _ := token.Subject()
		claims := bearerClaims{Sub: sub}
		var rawGroups []string
		if err := token.Get("cognito:groups", &rawGroups); err == nil {
			claims.Groups = rawGroups
		}

		ctx := context.WithValue(r.Context(), bearerClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// apiBearerRequireGroup restricts a Bearer-authenticated API route to users
// who belong to the given Cognito group. Returns JSON 401/403 on failure.
func (s *Service) apiBearerRequireGroup(group string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.apiBearerAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(bearerClaimsKey).(bearerClaims)
			if !ok {
				s.handleAPIError(w, r, ErrUnauthorized("unauthorized"))
				return
			}
			for _, g := range claims.Groups {
				if g == group {
					next.ServeHTTP(w, r)
					return
				}
			}
			s.handleAPIError(w, r, ErrForbidden("forbidden"))
		}))
	}
}

// noSurf sets up CSRF protection with secure cookie attributes.
func noSurf(next http.Handler) http.Handler {
	csrfHandler := nosurf.New(next)
	csrfHandler.SetBaseCookie(http.Cookie{
		HttpOnly: true,
		Path:     "/",
		Secure:   true,
	})
	csrfHandler.ExemptGlob("/api/*")
	return csrfHandler
}
