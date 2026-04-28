package service

import (
	"context"
	"fmt"
	"gomario/lib/config"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// jwksCache holds a cached, auto-refreshing JWKS keyset for the Cognito User Pool.
type jwksCache struct {
	cache  *jwk.Cache
	keyURL string
}

func newJWKSCache(ctx context.Context, cfg *config.Config) (*jwksCache, error) {
	keyURL := fmt.Sprintf(
		"https://cognito-idp.%s.amazonaws.com/%s/.well-known/jwks.json",
		cfg.Cognito.Region,
		cfg.Cognito.UserPoolID,
	)

	c, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("jwks: create cache: %w", err)
	}

	if err := c.Register(ctx, keyURL); err != nil {
		return nil, fmt.Errorf("jwks: register %s: %w", keyURL, err)
	}

	// Pre-warm the cache so the first real request doesn't block.
	if _, err := c.Refresh(ctx, keyURL); err != nil {
		return nil, fmt.Errorf("jwks: initial fetch from %s: %w", keyURL, err)
	}

	return &jwksCache{cache: c, keyURL: keyURL}, nil
}

// validateIDToken parses and validates a raw Cognito ID token JWT.
// It returns the validated token on success so callers can read claims.
func (jc *jwksCache) validateIDToken(ctx context.Context, rawToken string, clientID string, region string, userPoolID string) (jwt.Token, error) {
	keyset, err := jc.cache.Lookup(ctx, jc.keyURL)
	if err != nil {
		return nil, fmt.Errorf("jwks: lookup keyset: %w", err)
	}

	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID)

	token, err := jwt.Parse(
		[]byte(rawToken),
		jwt.WithKeySet(keyset),
		jwt.WithValidate(true),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(clientID),
	)
	if err != nil {
		return nil, fmt.Errorf("jwks: validate token: %w", err)
	}

	return token, nil
}

// validateAccessToken parses and validates a raw Cognito access token JWT.
// Unlike ID tokens, Cognito access tokens carry client_id (not aud) and have
// token_use == "access". Audience validation is intentionally omitted here.
func (jc *jwksCache) validateAccessToken(ctx context.Context, rawToken string, region string, userPoolID string) (jwt.Token, error) {
	keyset, err := jc.cache.Lookup(ctx, jc.keyURL)
	if err != nil {
		return nil, fmt.Errorf("jwks: lookup keyset: %w", err)
	}

	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID)

	token, err := jwt.Parse(
		[]byte(rawToken),
		jwt.WithKeySet(keyset),
		jwt.WithValidate(true),
		jwt.WithIssuer(issuer),
	)
	if err != nil {
		return nil, fmt.Errorf("jwks: validate access token: %w", err)
	}

	var tokenUse string
	if err := token.Get("token_use", &tokenUse); err != nil || tokenUse != "access" {
		return nil, fmt.Errorf("jwks: expected access token, got %q", tokenUse)
	}

	return token, nil
}
