package service

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"

	"github.com/justinas/nosurf"
	"golang.org/x/oauth2"
)

// cognitoOAuth2Config builds an oauth2.Config from the Cognito settings.
func (s *Service) cognitoOAuth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.Config.Cognito.ClientID,
		ClientSecret: s.Config.Cognito.ClientSecret,
		RedirectURL:  s.Config.Cognito.CallbackURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  fmt.Sprintf("https://%s/oauth2/authorize", s.Config.Cognito.Domain),
			TokenURL: fmt.Sprintf("https://%s/oauth2/token", s.Config.Cognito.Domain),
		},
	}
}

// login redirects the user to the Cognito Hosted UI to begin the OAuth flow.
// A random state value is stored in the session to prevent CSRF.
func (s *Service) login(w http.ResponseWriter, r *http.Request) error {
	state, err := randomState()
	if err != nil {
		return err
	}
	s.SessionManager.Put(r.Context(), "oauthState", state)

	authURL := s.cognitoOAuth2Config().AuthCodeURL(state)

	s.Logger.Info("Auth URL", "url", authURL)
	http.Redirect(w, r, authURL, http.StatusFound)
	return nil
}

// authCallback handles the redirect back from Cognito after the user authenticates.
// It validates the state param, exchanges the auth code for tokens, validates the
// ID token JWT, and writes the user's identity into the session.
func (s *Service) authCallback(w http.ResponseWriter, r *http.Request) error {
	// Validate the state param to prevent CSRF.
	storedState := s.SessionManager.GetString(r.Context(), "oauthState")
	if storedState == "" || storedState != r.URL.Query().Get("state") {
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return nil
	}
	s.SessionManager.Remove(r.Context(), "oauthState")

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return nil
	}

	// Exchange the authorization code for tokens.
	tokens, err := s.cognitoOAuth2Config().Exchange(r.Context(), code)
	if err != nil {
		return fmt.Errorf("authCallback: exchange code: %w", err)
	}

	idTokenStr, ok := tokens.Extra("id_token").(string)
	if !ok || idTokenStr == "" {
		return fmt.Errorf("authCallback: id_token missing from token response")
	}

	// Validate the ID token and extract claims.
	idToken, err := s.JWKSCache.validateIDToken(
		r.Context(),
		idTokenStr,
		s.Config.Cognito.ClientID,
		s.Config.Cognito.Region,
		s.Config.Cognito.UserPoolID,
	)
	if err != nil {
		return fmt.Errorf("authCallback: validate id token: %w", err)
	}

	sub, _ := idToken.Subject()

	var emailStr string
	_ = idToken.Get("email", &emailStr)

	// Cognito groups come in the cognito:groups claim as []string.
	var groups []string
	_ = idToken.Get("cognito:groups", &groups)

	// Renew the session token to prevent session fixation.
	if err := s.SessionManager.RenewToken(r.Context()); err != nil {
		return fmt.Errorf("authCallback: renew session: %w", err)
	}

	s.SessionManager.Put(r.Context(), "isAuthenticated", true)
	s.SessionManager.Put(r.Context(), "userSub", sub)
	s.SessionManager.Put(r.Context(), "userEmail", emailStr)
	s.SessionManager.Put(r.Context(), "userGroups", groups)

	redirectTo := s.SessionManager.PopString(r.Context(), "redirectAfterLogin")
	if redirectTo == "" {
		redirectTo = "/"
	}
	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
	return nil
}

// logout clears the local session and redirects to Cognito's logout endpoint
// so the Cognito SSO session is also terminated.
func (s *Service) logout(w http.ResponseWriter, r *http.Request) error {
	if err := s.SessionManager.Destroy(r.Context()); err != nil {
		return fmt.Errorf("logout: destroy session: %w", err)
	}

	cognitoLogoutURL := fmt.Sprintf("https://%s/logout?%s",
		s.Config.Cognito.Domain,
		url.Values{
			"client_id":  {s.Config.Cognito.ClientID},
			"logout_uri": {s.Config.Cognito.LogoutURL},
		}.Encode(),
	)

	http.Redirect(w, r, cognitoLogoutURL, http.StatusSeeOther)
	return nil
}

// csrfToken returns the current masked CSRF token as JSON so JavaScript can
// include it as the X-CSRF-Token header on state-mutating API requests.
func (s *Service) csrfToken(w http.ResponseWriter, r *http.Request) error {
	token := nosurf.Token(r)
	return s.renderJSON(w, map[string]string{"csrfToken": token}, http.StatusOK)
}

// randomState generates a cryptographically random base64-URL string for use
// as an OAuth state parameter.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomState: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
