package web

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

func TestOIDCPKCEOneTimeTransactionAndRevocableSession(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	var mu sync.Mutex
	var expectedNonce, expectedChallenge string
	tokenCalls := 0

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSON(w, http.StatusOK, map[string]any{
				"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
				"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks",
				"end_session_endpoint":     issuer + "/end-session",
				"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
				"code_challenge_methods_supported":      []string{"S256"},
			})
		case "/jwks":
			e := big.NewInt(int64(privateKey.PublicKey.E)).Bytes()
			writeJSON(w, http.StatusOK, map[string]any{"keys": []any{map[string]any{
				"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(privateKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(e),
			}}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			tokenCalls++
			nonce, challenge := expectedNonce, expectedChallenge
			mu.Unlock()
			verifier := r.Form.Get("code_verifier")
			digest := sha256.Sum256([]byte(verifier))
			if verifier == "" || base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
				http.Error(w, "invalid PKCE verifier", http.StatusBadRequest)
				return
			}
			idToken := signTestIDToken(t, privateKey, map[string]any{
				"iss": issuer, "sub": "subject-1", "aud": "llm-proxy-web",
				"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix(),
				"nonce": nonce, "preferred_username": "alice", "email": "alice@example.test",
			})
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": "redacted-test-access-token", "token_type": "Bearer", "expires_in": 3600,
				"id_token": idToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer idp.Close()
	issuer = idp.URL

	key, err := vault.GenerateKeyBase64()
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.NewFromBase64(key)
	if err != nil {
		t.Fatal(err)
	}
	st := memory.New()
	srv, err := NewServer(context.Background(), Config{
		Store: st, Vault: v, SessionSecret: "0123456789abcdef",
		SessionMaxAge: time.Hour, SessionIdleTimeout: 15 * time.Minute,
		OIDC: &OIDCConfig{IssuerURL: issuer, ClientID: "llm-proxy-web", PublicURL: "http://proxy.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.Register(mux)

	login := httptest.NewRequest(http.MethodGet, "/login?return_to=/app/tokens", nil)
	loginResult := httptest.NewRecorder()
	mux.ServeHTTP(loginResult, login)
	if loginResult.Code != http.StatusFound {
		t.Fatalf("login = %d %s", loginResult.Code, loginResult.Body.String())
	}
	location, err := url.Parse(loginResult.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	query := location.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") == "" {
		t.Fatalf("PKCE query = %s", location.RawQuery)
	}
	if query.Get("state") == "" || query.Get("nonce") == "" {
		t.Fatalf("state/nonce missing: %s", location.RawQuery)
	}
	var transactionCookie *http.Cookie
	for _, cookie := range loginResult.Result().Cookies() {
		if cookie.Name == authTransactionCookie {
			transactionCookie = cookie
		}
		if strings.Contains(cookie.Name, "nonce") || strings.Contains(cookie.Name, "state") {
			t.Fatalf("sensitive auth cookie emitted: %s", cookie.Name)
		}
	}
	if transactionCookie == nil {
		t.Fatal("auth transaction cookie missing")
	}
	mu.Lock()
	expectedNonce, expectedChallenge = query.Get("nonce"), query.Get("code_challenge")
	mu.Unlock()

	callbackPath := "/auth/callback?code=one-time-code&state=" + url.QueryEscape(query.Get("state"))
	callback := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	callback.AddCookie(transactionCookie)
	callbackResult := httptest.NewRecorder()
	mux.ServeHTTP(callbackResult, callback)
	if callbackResult.Code != http.StatusFound || callbackResult.Header().Get("Location") != "/app/tokens" {
		t.Fatalf("callback = %d location=%q body=%s", callbackResult.Code, callbackResult.Header().Get("Location"), callbackResult.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range callbackResult.Result().Cookies() {
		if cookie.Name == SessionCookieName {
			sessionCookie = cookie
		}
	}
	var transactionCookieCleared bool
	for _, header := range callbackResult.Header().Values("Set-Cookie") {
		if strings.HasPrefix(header, authTransactionCookie+"=;") && strings.Contains(header, "Expires=Thu, 01 Jan 1970") && strings.Contains(header, "Max-Age=0") {
			transactionCookieCleared = true
		}
	}
	if !transactionCookieCleared {
		t.Fatal("auth transaction cookie was not expired after callback")
	}
	if sessionCookie == nil {
		t.Fatal("server-side session cookie missing")
	}
	if strings.Contains(sessionCookie.Value, "alice") || strings.Contains(sessionCookie.Value, "subject-1") {
		t.Fatal("session cookie contains identity claims")
	}
	user, err := st.GetUserByIdentity(context.Background(), issuer, "subject-1")
	if err != nil || user.Username != "alice" {
		t.Fatalf("issuer-aware user = %+v, %v", user, err)
	}

	replay := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	replay.AddCookie(transactionCookie)
	replayResult := httptest.NewRecorder()
	mux.ServeHTTP(replayResult, replay)
	if replayResult.Code != http.StatusBadRequest {
		t.Fatalf("replay = %d", replayResult.Code)
	}
	mu.Lock()
	calls := tokenCalls
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("token endpoint calls after replay = %d", calls)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	me.AddCookie(sessionCookie)
	meResult := httptest.NewRecorder()
	mux.ServeHTTP(meResult, me)
	if meResult.Code != http.StatusOK {
		t.Fatalf("session use = %d %s", meResult.Code, meResult.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "/logout", nil)
	logout.AddCookie(sessionCookie)
	logoutResult := httptest.NewRecorder()
	mux.ServeHTTP(logoutResult, logout)
	if logoutResult.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d", logoutResult.Code)
	}
	logoutLocation, err := url.Parse(logoutResult.Header().Get("Location"))
	if err != nil || logoutLocation.Path != "/end-session" || logoutLocation.Query().Get("client_id") != "llm-proxy-web" || logoutLocation.Query().Get("post_logout_redirect_uri") != "http://proxy.example/" {
		t.Fatalf("provider logout URL = %q, %v", logoutResult.Header().Get("Location"), err)
	}
	meAgain := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meAgain.AddCookie(sessionCookie)
	meAgainResult := httptest.NewRecorder()
	mux.ServeHTTP(meAgainResult, meAgain)
	if meAgainResult.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session = %d", meAgainResult.Code)
	}
}

func TestOIDCRejectsCrossOriginEndSessionEndpoint(t *testing.T) {
	var issuer string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
			"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks",
			"end_session_endpoint":     "https://untrusted.example/end-session",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer idp.Close()
	issuer = idp.URL
	if _, err := newOIDCClient(context.Background(), OIDCConfig{IssuerURL: issuer, ClientID: "client", PublicURL: "http://proxy.example"}); err == nil || !strings.Contains(err.Error(), "end-session endpoint") {
		t.Fatalf("cross-origin end-session endpoint error = %v", err)
	}
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s.%s", unsigned, base64.RawURLEncoding.EncodeToString(signature))
}
