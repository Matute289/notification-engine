// Package auth — Clerk JWT verifier using remote JWKS.
//
// ClerkVerifier fetches Clerk's public key set once at startup, caches it with
// automatic background refresh, and verifies RS256-signed JWTs locally — one
// network call at boot time, zero per request (under normal operation).
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// ClerkClaims holds the verified claims we care about from a Clerk session token.
type ClerkClaims struct {
	Subject string // "sub" — Clerk user ID
}

// ClerkVerifier verifies Clerk session JWTs. A nil pointer means Clerk auth is
// disabled — callers should check before invoking Verify.
type ClerkVerifier struct {
	cache            *jwk.Cache
	jwksURL          string
	issuer           string
	authorizedParties []string
}

// NewClerkVerifier builds a ClerkVerifier for the given Clerk instance.
//
// issuer is the Clerk Frontend API URL (e.g. "https://<slug>.clerk.accounts.dev").
// The JWKS URL is derived as issuer + "/.well-known/jwks.json".
// If issuer is empty, NewClerkVerifier returns (nil, nil) — Clerk auth is disabled
// and the caller should rely on HMAC only.
// authorizedParties (azp) can be nil to skip audience validation.
func NewClerkVerifier(ctx context.Context, issuer string, authorizedParties []string) (*ClerkVerifier, error) {
	if issuer == "" {
		return nil, nil
	}
	jwksURL := issuer + "/.well-known/jwks.json"

	cache := jwk.NewCache(ctx)
	if err := cache.Register(jwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
		return nil, fmt.Errorf("clerk: register JWKS endpoint: %w", err)
	}
	// Eagerly fetch keys so a bad issuer URL fails at startup, not on first request.
	if _, err := cache.Refresh(ctx, jwksURL); err != nil {
		return nil, fmt.Errorf("clerk: initial JWKS fetch from %s: %w", jwksURL, err)
	}

	return &ClerkVerifier{
		cache:            cache,
		jwksURL:          jwksURL,
		issuer:           issuer,
		authorizedParties: authorizedParties,
	}, nil
}

// Verify validates a raw Clerk JWT string and returns the verified claims.
func (cv *ClerkVerifier) Verify(ctx context.Context, token string) (ClerkClaims, error) {
	keyset, err := cv.cache.Get(ctx, cv.jwksURL)
	if err != nil {
		return ClerkClaims{}, fmt.Errorf("clerk: get keyset: %w", err)
	}

	tok, err := jwt.Parse(
		[]byte(token),
		jwt.WithKeySet(keyset),
		jwt.WithIssuer(cv.issuer),
		jwt.WithValidate(true),
	)
	if err != nil {
		return ClerkClaims{}, fmt.Errorf("clerk: invalid token: %w", err)
	}

	if len(cv.authorizedParties) > 0 {
		if azpStr, _ := tok.PrivateClaims()["azp"].(string); azpStr != "" {
			if !containsString(cv.authorizedParties, azpStr) {
				return ClerkClaims{}, fmt.Errorf("clerk: unauthorized party %q", azpStr)
			}
		}
	}

	return ClerkClaims{Subject: tok.Subject()}, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
