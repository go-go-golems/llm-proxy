package oidcauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type introspectionFixture struct {
	server    *httptest.Server
	mu        sync.Mutex
	calls     int
	responses map[string]map[string]any
	status    int
}

func newFixture(t *testing.T, endpointOverride string) *introspectionFixture {
	t.Helper()
	fixture := &introspectionFixture{responses: map[string]map[string]any{}, status: http.StatusOK}
	var issuer string
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			endpoint := issuer + "/introspect"
			if endpointOverride != "" {
				endpoint = endpointOverride
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": issuer, "introspection_endpoint": endpoint,
				"introspection_endpoint_auth_methods_supported": []string{"client_secret_basic"},
			})
		case "/introspect":
			fixture.mu.Lock()
			fixture.calls++
			status := fixture.status
			fixture.mu.Unlock()
			clientID, secret, ok := r.BasicAuth()
			clientID, clientIDErr := url.QueryUnescape(clientID)
			secret, secretErr := url.QueryUnescape(secret)
			if !ok || clientIDErr != nil || secretErr != nil || clientID != "resource" || secret != "resource+secret/%" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if status != http.StatusOK {
				http.Error(w, "unavailable", status)
				return
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			fixture.mu.Lock()
			body, ok := fixture.responses[r.Form.Get("token")]
			fixture.mu.Unlock()
			if !ok {
				body = map[string]any{"active": false}
			}
			_ = json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	issuer = fixture.server.URL
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *introspectionFixture) config() Config {
	return Config{
		IssuerURL: f.server.URL, ResourceClientID: "resource", ClientSecret: []byte("resource+secret/%"),
		Audience: "https://proxy.example/agent/v1", AllowedClients: []string{"agent"}, HTTPClient: f.server.Client(),
		PositiveCacheTTL: time.Second, NegativeCacheTTL: time.Second,
	}
}

func (f *introspectionFixture) valid(subject string) map[string]any {
	return map[string]any{
		"active": true, "iss": f.server.URL, "sub": subject, "client_id": "agent",
		"scope": "openid llm.tokens.issue", "aud": []string{"https://proxy.example/agent/v1"},
		"exp": time.Now().Add(time.Hour).Unix(), "token_type": "Bearer",
	}
}

func TestRejectsInsecureIssuerAndAudienceBeforeCredentials(t *testing.T) {
	config := Config{IssuerURL: "http://idp.example", ResourceClientID: "resource", ClientSecret: []byte("not-a-real-secret"), Audience: "https://broker.example/agent/v1", AllowedClients: []string{"agent"}}
	if _, err := New(context.Background(), config); err == nil {
		t.Fatal("insecure issuer accepted")
	}
	config.IssuerURL = "https://idp.example"
	config.Audience = "http://broker.example/agent/v1"
	if _, err := New(context.Background(), config); err == nil {
		t.Fatal("insecure audience accepted")
	}
}

func TestAuthenticateValidatesIntrospectionAndCachesByDigest(t *testing.T) {
	fixture := newFixture(t, "")
	fixture.responses["opaque-access"] = fixture.valid("subject")
	authenticator, err := New(context.Background(), fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		principal, failure := authenticator.Authenticate(context.Background(), []string{"Bearer opaque-access"}, []string{"llm.tokens.issue"})
		if failure != nil || principal.Subject != "subject" || principal.ClientID != "agent" {
			t.Fatalf("principal = %+v, %v", principal, failure)
		}
	}
	fixture.mu.Lock()
	calls := fixture.calls
	fixture.mu.Unlock()
	if calls != 1 {
		t.Fatalf("introspection calls = %d, want one cached call", calls)
	}
	for key := range authenticator.cache {
		if strings.Contains(key, "opaque-access") {
			t.Fatal("raw token used as cache key")
		}
	}
}

func TestAuthenticateFailureClassificationAndClaims(t *testing.T) {
	fixture := newFixture(t, "")
	valid := fixture.valid("subject")
	fixture.responses["valid"] = valid
	fixture.responses["inactive"] = map[string]any{"active": false}
	wrongIssuer := fixture.valid("subject")
	wrongIssuer["iss"] = "https://other.example"
	fixture.responses["wrong-issuer"] = wrongIssuer
	wrongAudience := fixture.valid("subject")
	wrongAudience["aud"] = []string{"other"}
	fixture.responses["wrong-audience"] = wrongAudience
	wrongClient := fixture.valid("subject")
	wrongClient["client_id"] = "other"
	fixture.responses["wrong-client"] = wrongClient
	expired := fixture.valid("subject")
	expired["exp"] = time.Now().Add(-time.Minute).Unix()
	fixture.responses["expired"] = expired
	wrongType := fixture.valid("subject")
	wrongType["token_type"] = "DPoP"
	fixture.responses["wrong-type"] = wrongType
	authenticator, err := New(context.Background(), fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"inactive", "wrong-issuer", "wrong-audience", "wrong-client", "expired", "wrong-type"} {
		if _, failure := authenticator.Authenticate(context.Background(), []string{"Bearer " + token}, nil); failure == nil || failure.Status != http.StatusUnauthorized || failure.Code != "invalid_token" {
			t.Fatalf("%s failure = %+v", token, failure)
		}
	}
	if _, failure := authenticator.Authenticate(context.Background(), []string{"Bearer valid"}, []string{"missing"}); failure == nil || failure.Status != http.StatusForbidden {
		t.Fatalf("scope failure = %+v", failure)
	}
	for _, headers := range [][]string{nil, {"Basic value"}, {"Bearer one", "Bearer two"}} {
		if _, failure := authenticator.Authenticate(context.Background(), headers, nil); failure == nil || failure.Status != http.StatusUnauthorized {
			t.Fatalf("header failure = %+v", failure)
		}
	}
}

func TestAuthenticateTreatsIntrospectionFailureAsUnavailable(t *testing.T) {
	fixture := newFixture(t, "")
	fixture.mu.Lock()
	fixture.status = http.StatusInternalServerError
	fixture.mu.Unlock()
	authenticator, err := New(context.Background(), fixture.config())
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := authenticator.Authenticate(context.Background(), []string{"Bearer opaque"}, nil); failure == nil || failure.Status != http.StatusServiceUnavailable {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestNewRejectsCrossOriginIntrospectionEndpoint(t *testing.T) {
	fixture := newFixture(t, "https://untrusted.example/introspect")
	if _, err := New(context.Background(), fixture.config()); err == nil || !strings.Contains(err.Error(), "issuer origin") {
		t.Fatalf("error = %v", err)
	}
}
