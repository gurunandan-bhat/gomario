package service

import (
	"context"
	"encoding/json"
	"fmt"
	"gomario/lib/config"
	"gomario/lib/model"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Service struct {
	Config         *config.Config
	Muxer          *chi.Mux
	Template       map[string]*template.Template
	SessionManager *scs.SessionManager
	Model          *model.Model
	Logger         *slog.Logger
	JWKSCache      *jwksCache
}

// var csrfProtector *http.CrossOriginProtection

func NewService(cfg *config.Config) (*Service, error) {

	mux := chi.NewRouter()

	mux.Use(middleware.Recoverer)
	mux.Use(middleware.RequestID)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mux.Use(slogMiddleware(cfg, logger))

	mux.Use(middleware.RealIP)

	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	m, err := model.NewModel(cfg)
	if err != nil {
		log.Fatalf("error connecting to database: %s", err)
	}

	sessionMgr := scs.New()
	sessionMgr.Store = mysqlstore.New(m.DbHandle.DB)
	sessionMgr.Lifetime = 12 * time.Hour
	sessionMgr.Cookie.Name = cfg.Session.CookieName
	sessionMgr.Cookie.HttpOnly = true
	sessionMgr.Cookie.SameSite = http.SameSiteStrictMode
	sessionMgr.Cookie.Secure = cfg.IsProduction

	mux.Use(sessionMgr.LoadAndSave)
	mux.Use(noSurf)

	// Static file handler
	// IMP: This should come *AFTER* all
	// middleware are set via mux.Use
	filesDir := http.Dir(filepath.Join(cfg.AppRoot, "assets"))
	fs := http.FileServer(filesDir)
	mux.Handle("/assets/*", http.StripPrefix("/assets", fs))

	template, err := newTemplateCache(filepath.Join(cfg.AppRoot, "templates"))
	if err != nil {
		log.Fatalf("Cannot build template cache: %s", err)
	}

	jwksCache, err := newJWKSCache(context.Background(), cfg)
	if err != nil {
		log.Fatalf("Cannot initialize JWKS cache: %s", err)
	}

	s := &Service{
		Config:         cfg,
		Muxer:          mux,
		Template:       template,
		SessionManager: sessionMgr,
		Model:          m,
		Logger:         logger,
		JWKSCache:      jwksCache,
	}

	s.setRoutes()

	return s, nil
}

func (s *Service) setRoutes() {

	s.Muxer.Method(http.MethodGet, "/", serviceHandler(s.index))
	s.Muxer.Method(http.MethodGet, "/start", serviceHandler(s.start))

	// Auth routes — not protected by requireAuth.
	s.Muxer.Method(http.MethodGet, "/login", serviceHandler(s.login))
	s.Muxer.Method(http.MethodGet, "/auth/callback", serviceHandler(s.authCallback))
	s.Muxer.Method(http.MethodGet, "/logout", serviceHandler(s.logout))

	// JSON API routes — all require authentication; group-restricted routes
	// use r.With(s.apiRequireGroup("group-name")) per endpoint.
	s.Muxer.Route("/api", func(r chi.Router) {
		r.Use(s.apiAuthMiddleware)

		// Returns the current CSRF token so JavaScript can include it as
		// X-CSRF-Token on state-mutating requests (POST/PUT/DELETE).
		r.Method(http.MethodGet, "/csrf-token", apiHandler(s.csrfToken))
	})
}

// renderJSON serialises data as JSON and writes it with the given status code.
func (s *Service) renderJSON(w http.ResponseWriter, data any, status int) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("renderJSON: marshal: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		s.Logger.Error("renderJSON: write", "error", err)
	}
	return nil
}

func (s *Service) renderJSONError(w http.ResponseWriter, message string, status int) {

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	body := struct {
		Message string `json:"message"`
	}{Message: message}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		s.Logger.Error("Error marshaling error response", "error", err)
	}

	if _, err := w.Write(jsonBody); err != nil {
		s.Logger.Error("Error writing to response writer", "error", err)
	}
}
