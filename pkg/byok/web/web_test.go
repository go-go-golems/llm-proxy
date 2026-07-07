package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
	"github.com/go-go-golems/llm-proxy/pkg/byok/web"
)

func TestSessionCodecRoundTripAndTamper(t *testing.T) {
	codec, err := web.NewSessionCodec("0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("codec: %v", err)
	}
	value, err := codec.Encode(web.SessionClaims{Subject: "s", Username: "alice", UserID: "u1"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	claims, err := codec.Decode(value)
	if err != nil || claims.Username != "alice" || claims.UserID != "u1" {
		t.Fatalf("decode: %v %+v", err, claims)
	}
	// Tampered payload fails signature verification.
	parts := strings.SplitN(value, ".", 2)
	tampered := parts[0][:len(parts[0])-2] + "xx." + parts[1]
	if _, err := codec.Decode(tampered); err == nil {
		t.Fatal("tampered session accepted")
	}
	// Different secret fails.
	other, _ := web.NewSessionCodec("fedcba9876543210", time.Hour)
	if _, err := other.Decode(value); err == nil {
		t.Fatal("session accepted under different secret")
	}
}

type webFixture struct {
	mux    *http.ServeMux
	store  store.Store
	cookie *http.Cookie
	user   store.User
}

func newWebFixture(t *testing.T) *webFixture {
	t.Helper()
	ctx := context.Background()
	st := memory.New()
	keyB64, err := vault.GenerateKeyBase64()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	v, err := vault.NewFromBase64(keyB64)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}
	srv, err := web.NewServer(ctx, web.Config{
		Store: st, Vault: v, SessionSecret: "0123456789abcdef", DevUser: "alice",
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	mux := http.NewServeMux()
	srv.Register(mux)

	// Log in through /dev-login to obtain a session cookie.
	req := httptest.NewRequest(http.MethodGet, "/dev-login", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("dev-login: %d %s", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == web.SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie from dev-login")
	}
	user, err := st.GetUserBySubject(ctx, "local:alice")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	return &webFixture{mux: mux, store: st, cookie: cookie, user: user}
}

func (f *webFixture) do(t *testing.T, method, path, body string, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if withSession {
		req.AddCookie(f.cookie)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}

func TestAPIRequiresSession(t *testing.T) {
	f := newWebFixture(t)
	if w := f.do(t, http.MethodGet, "/api/credentials", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", w.Code)
	}
}

func TestCredentialAndTokenLifecycleViaAPI(t *testing.T) {
	f := newWebFixture(t)

	// Create a credential.
	w := f.do(t, http.MethodPost, "/api/credentials",
		`{"provider":"anthropic","api_type":"claude","label":"personal","secret":"sk-ant-supersecret"}`, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("create credential: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "supersecret") {
		t.Fatal("secret leaked in credential response")
	}
	var cred struct {
		ID          string `json:"id"`
		SecretLast4 string `json:"secret_last4"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cred); err != nil || cred.ID == "" {
		t.Fatalf("credential response: %v %s", err, w.Body.String())
	}
	if cred.SecretLast4 != "…cret" {
		t.Fatalf("secret_last4: %q", cred.SecretLast4)
	}

	// Stored ciphertext must not contain the plaintext.
	stored, err := f.store.GetCredential(context.Background(), f.user.ID, cred.ID)
	if err != nil {
		t.Fatalf("stored credential: %v", err)
	}
	if strings.Contains(string(stored.SecretCipher), "supersecret") {
		t.Fatal("credential stored unencrypted")
	}

	// Mint a token bound to it.
	w = f.do(t, http.MethodPost, "/api/tokens",
		`{"name":"demo","credential_ids":["`+cred.ID+`"],"allowed_models":["sonnet"],"max_total_tokens":5000}`, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("mint: %d %s", w.Code, w.Body.String())
	}
	var minted struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &minted); err != nil {
		t.Fatalf("mint response: %v", err)
	}
	if !strings.HasPrefix(minted.Token, "llmp_") {
		t.Fatalf("mint response missing plaintext token: %q", minted.Token)
	}

	// List does NOT include the plaintext.
	w = f.do(t, http.MethodGet, "/api/tokens", "", true)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), minted.Token) {
		t.Fatalf("token list leaked plaintext: %d", w.Code)
	}

	// Usage endpoint works and is ownership-checked.
	w = f.do(t, http.MethodGet, "/api/usage?token_id="+minted.ID, "", true)
	if w.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", w.Code, w.Body.String())
	}
	if w := f.do(t, http.MethodGet, "/api/usage?token_id=someone-elses", "", true); w.Code != http.StatusNotFound {
		t.Fatalf("usage ownership: %d", w.Code)
	}

	// Revoke.
	if w := f.do(t, http.MethodPost, "/api/tokens/"+minted.ID+"/revoke", "", true); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}

	// Deleting the credential revokes nothing further (token already revoked)
	// and removes the row.
	if w := f.do(t, http.MethodDelete, "/api/credentials/"+cred.ID, "", true); w.Code != http.StatusNoContent {
		t.Fatalf("delete credential: %d %s", w.Code, w.Body.String())
	}
}

func TestMintRequiresCredentials(t *testing.T) {
	f := newWebFixture(t)
	w := f.do(t, http.MethodPost, "/api/tokens",
		`{"name":"demo","credential_ids":[],"allowed_models":["sonnet"]}`, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing credentials, got %d %s", w.Code, w.Body.String())
	}
	w = f.do(t, http.MethodPost, "/api/tokens",
		`{"name":"demo","credential_ids":["nope"],"allowed_models":["sonnet"]}`, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown credential, got %d %s", w.Code, w.Body.String())
	}
}

func TestCrossOriginMutationRejected(t *testing.T) {
	f := newWebFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/credentials",
		strings.NewReader(`{"provider":"x","api_type":"y","secret":"z"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	req.AddCookie(f.cookie)
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation not rejected: %d", w.Code)
	}
}
