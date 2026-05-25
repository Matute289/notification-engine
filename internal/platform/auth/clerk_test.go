package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/require"
)

// rsaKey generates a fresh RSA key pair for each test run.
func rsaKey(t *testing.T) (*rsa.PrivateKey, jwk.Key) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pub, err := jwk.FromRaw(priv.Public())
	require.NoError(t, err)
	require.NoError(t, pub.Set(jwk.KeyIDKey, "test-kid"))
	require.NoError(t, pub.Set(jwk.AlgorithmKey, jwa.RS256))

	return priv, pub
}

// jwksServer spins up an httptest server serving the given public key as a JWKS.
func jwksServer(t *testing.T, pubKey jwk.Key) *httptest.Server {
	t.Helper()
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(pubKey))
	raw, err := json.Marshal(set)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// signToken builds and signs a JWT with the given private key.
func signToken(t *testing.T, priv *rsa.PrivateKey, issuer, subject, azp string, exp time.Time) string {
	t.Helper()
	b := jwt.NewBuilder().
		Issuer(issuer).
		Subject(subject).
		IssuedAt(time.Now()).
		Expiration(exp)
	if azp != "" {
		b = b.Claim("azp", azp)
	}
	tok, err := b.Build()
	require.NoError(t, err)

	privKey, err := jwk.FromRaw(priv)
	require.NoError(t, err)
	require.NoError(t, privKey.Set(jwk.KeyIDKey, "test-kid"))
	require.NoError(t, privKey.Set(jwk.AlgorithmKey, jwa.RS256))

	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256, privKey))
	require.NoError(t, err)
	return string(signed)
}

func newVerifierForTest(t *testing.T, srv *httptest.Server, authorizedParties []string) *ClerkVerifier {
	t.Helper()
	issuer := srv.URL
	// Override jwksURL to point at /jwks path on the test server.
	ctx := context.Background()
	cache := jwk.NewCache(ctx)
	// Register the test server's URL directly as jwksURL.
	jwksURL := srv.URL + "/.well-known/jwks.json"
	require.NoError(t, cache.Register(jwksURL, jwk.WithMinRefreshInterval(0)))
	_, err := cache.Refresh(ctx, jwksURL)
	require.NoError(t, err)

	return &ClerkVerifier{
		cache:            cache,
		jwksURL:          jwksURL,
		issuer:           issuer,
		authorizedParties: authorizedParties,
	}
}

// jwksServerWithPath serves JWKS at "/.well-known/jwks.json".
func jwksServerWithPath(t *testing.T, pubKey jwk.Key) *httptest.Server {
	t.Helper()
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(pubKey))
	raw, err := json.Marshal(set)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newVerifierViaConstructor uses the real NewClerkVerifier constructor against a test server.
func newVerifierViaConstructor(t *testing.T, srv *httptest.Server, authorizedParties []string) *ClerkVerifier {
	t.Helper()
	cv, err := NewClerkVerifier(context.Background(), srv.URL, authorizedParties)
	require.NoError(t, err)
	require.NotNil(t, cv)
	return cv
}

func TestClerkVerify_RoundTrip(t *testing.T) {
	priv, pub := rsaKey(t)
	srv := jwksServerWithPath(t, pub)
	cv := newVerifierViaConstructor(t, srv, nil)

	token := signToken(t, priv, srv.URL, "user_123", "", time.Now().Add(time.Hour))
	claims, err := cv.Verify(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "user_123", claims.Subject)
}

func TestClerkVerify_Expired(t *testing.T) {
	priv, pub := rsaKey(t)
	srv := jwksServerWithPath(t, pub)
	cv := newVerifierViaConstructor(t, srv, nil)

	token := signToken(t, priv, srv.URL, "user_123", "", time.Now().Add(-time.Minute))
	_, err := cv.Verify(context.Background(), token)
	require.Error(t, err)
}

func TestClerkVerify_WrongIssuer(t *testing.T) {
	priv, pub := rsaKey(t)
	srv := jwksServerWithPath(t, pub)
	cv := newVerifierViaConstructor(t, srv, nil)

	token := signToken(t, priv, "https://other.clerk.dev", "user_123", "", time.Now().Add(time.Hour))
	_, err := cv.Verify(context.Background(), token)
	require.Error(t, err)
}

func TestClerkVerify_BadSignature(t *testing.T) {
	_, pub := rsaKey(t)
	otherPriv, _ := rsaKey(t) // different key pair
	srv := jwksServerWithPath(t, pub)
	cv := newVerifierViaConstructor(t, srv, nil)

	// Sign with a different key — verification should fail.
	token := signToken(t, otherPriv, srv.URL, "user_123", "", time.Now().Add(time.Hour))
	_, err := cv.Verify(context.Background(), token)
	require.Error(t, err)
}

func TestClerkVerify_AuthorizedParty_OK(t *testing.T) {
	priv, pub := rsaKey(t)
	srv := jwksServerWithPath(t, pub)
	cv := newVerifierViaConstructor(t, srv, []string{"https://myapp.com"})

	token := signToken(t, priv, srv.URL, "user_123", "https://myapp.com", time.Now().Add(time.Hour))
	claims, err := cv.Verify(context.Background(), token)
	require.NoError(t, err)
	require.Equal(t, "user_123", claims.Subject)
}

func TestClerkVerify_AuthorizedParty_Rejected(t *testing.T) {
	priv, pub := rsaKey(t)
	srv := jwksServerWithPath(t, pub)
	cv := newVerifierViaConstructor(t, srv, []string{"https://myapp.com"})

	token := signToken(t, priv, srv.URL, "user_123", "https://evil.com", time.Now().Add(time.Hour))
	_, err := cv.Verify(context.Background(), token)
	require.Error(t, err)
}

func TestNewClerkVerifier_EmptyIssuer(t *testing.T) {
	cv, err := NewClerkVerifier(context.Background(), "", nil)
	require.NoError(t, err)
	require.Nil(t, cv, "empty issuer should return nil verifier")
}
