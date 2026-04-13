package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/justinas/nosurf"
)

// tokenResponse is the JSON body returned by Cognito's /oauth2/token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// exchangeCode exchanges an OAuth authorization code for tokens at Cognito's token endpoint.
func (s *Service) exchangeCode(ctx context.Context, code string) (*tokenResponse, error) {
	tokenURL := fmt.Sprintf("https://%s/oauth2/token", s.Config.Cognito.Domain)

	body := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {s.Config.Cognito.ClientID},
		"code":         {code},
		"redirect_uri": {s.Config.Cognito.CallbackURL},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("exchangeCode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.Config.Cognito.ClientID, s.Config.Cognito.ClientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchangeCode: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchangeCode: token endpoint returned %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("exchangeCode: decode response: %w", err)
	}
	return &tr, nil
}

// login redirects the user to the Cognito Hosted UI to begin the OAuth flow.
// A random state value is stored in the session to prevent CSRF.
func (s *Service) login(w http.ResponseWriter, r *http.Request) error {
	state, err := randomState()
	if err != nil {
		return err
	}
	s.SessionManager.Put(r.Context(), "oauthState", state)

	authURL := fmt.Sprintf("https://%s/oauth2/authorize?%s",
		s.Config.Cognito.Domain,
		url.Values{
			"response_type": {"code"},
			"client_id":     {s.Config.Cognito.ClientID},
			"redirect_uri":  {s.Config.Cognito.CallbackURL},
			"scope":         {"openid email profile"},
			"state":         {state},
		}.Encode(),
	)

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
	tokens, err := s.exchangeCode(r.Context(), code)
	if err != nil {
		return fmt.Errorf("authCallback: exchange code: %w", err)
	}

	// Validate the ID token and extract claims.
	idToken, err := s.JWKSCache.validateIDToken(
		r.Context(),
		tokens.IDToken,
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
