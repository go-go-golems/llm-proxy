package agentapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	"github.com/go-go-golems/llm-proxy/pkg/byok/oidcauth"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
)

type fakeAuth struct {
	principal  oidcauth.Principal
	failure    *oidcauth.Failure
	seenScopes []string
}

func (a *fakeAuth) Authenticate(_ context.Context, _ []string, scopes []string) (oidcauth.Principal, *oidcauth.Failure) {
	a.seenScopes = append([]string(nil), scopes...)
	return a.principal, a.failure
}

func TestListGrantsMapsExactIdentityAndHidesCredentialBindings(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	user, err := st.UpsertUser(ctx, store.User{OIDCIssuer: "https://issuer.example", OIDCSubject: "subject", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := st.CreateCredential(ctx, store.Credential{UserID: user.ID, Provider: "openai", APIType: "openai", Label: "secret label", SecretCipher: []byte{1}, SecretLast4: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := st.CreateAgentGrantAudited(ctx, store.AgentGrant{UserID: user.ID, Name: "laptop", CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model"}, TokenTTL: time.Hour, MaxActivePerInstance: 1})
	if err != nil {
		t.Fatal(err)
	}
	auth := &fakeAuth{principal: oidcauth.Principal{Issuer: user.OIDCIssuer, Subject: user.OIDCSubject, ClientID: "agent"}}
	server, err := New(st, auth)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.Register(mux)
	request := httptest.NewRequest(http.MethodGet, "/agent/v1/grants", nil)
	request.Header.Set("Authorization", "Bearer opaque")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), grant.ID) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), credential.ID) || strings.Contains(response.Body.String(), "secret label") {
		t.Fatal("credential binding leaked to agent")
	}
	if len(auth.seenScopes) != 1 || auth.seenScopes[0] != issueScope {
		t.Fatalf("required scopes = %v", auth.seenScopes)
	}

	var firstToken string
	for attempt := range 2 {
		request = httptest.NewRequest(http.MethodPost, "/agent/v1/tokens", strings.NewReader(`{"grant_id":"`+grant.ID+`","client_instance_id":"0123456789abcdef"}`))
		request.Header.Set("Authorization", "Bearer opaque")
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), credential.ID) {
			t.Fatalf("issue response %d = %d %s", attempt, response.Code, response.Body.String())
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !strings.HasPrefix(body.Token, "llmp_") {
			t.Fatalf("issued token = %q, %v", body.Token, err)
		}
		if attempt == 0 {
			firstToken = body.Token
		} else if body.Token == firstToken {
			t.Fatal("rotation replayed plaintext token")
		}
	}
	storedTokens, err := st.ListTokensByUser(ctx, user.ID)
	if err != nil || len(storedTokens) != 2 {
		t.Fatalf("tokens = %+v, %v", storedTokens, err)
	}
	var active int
	for _, token := range storedTokens {
		if token.RevokedAt == nil {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active rotated tokens = %d", active)
	}
}

type tokenClassAuth struct{ principal oidcauth.Principal }

func (a tokenClassAuth) Authenticate(_ context.Context, headers []string, _ []string) (oidcauth.Principal, *oidcauth.Failure) {
	if len(headers) == 1 && headers[0] == "Bearer tiny-access" {
		return a.principal, nil
	}
	return oidcauth.Principal{}, &oidcauth.Failure{Status: http.StatusUnauthorized, Code: "invalid_token"}
}

func TestTinyAndBrokerTokensAreRouteSeparated(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	user, _ := st.UpsertUser(ctx, store.User{OIDCIssuer: "https://issuer.example", OIDCSubject: "subject", Username: "alice"})
	credential, _ := st.CreateCredential(ctx, store.Credential{UserID: user.ID, Provider: "openai", APIType: "openai", Label: "agent", SecretCipher: []byte{1}, SecretLast4: "safe"})
	_, _ = st.CreateAgentGrantAudited(ctx, store.AgentGrant{UserID: user.ID, Name: "grant", CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model"}, TokenTTL: time.Hour, MaxActivePerInstance: 1})
	rawBroker, brokerHash, _ := tokens.Mint()
	_, _ = st.MintToken(ctx, store.Token{UserID: user.ID, TokenHash: brokerHash, Name: "broker", CredentialIDs: []string{credential.ID}, AllowedModels: []string{"model"}})
	agentServer, _ := New(st, tokenClassAuth{principal: oidcauth.Principal{Issuer: user.OIDCIssuer, Subject: user.OIDCSubject, ClientID: "agent"}})
	mux := http.NewServeMux()
	agentServer.Register(mux)
	mux.Handle("/", authmw.TokenAuth(st, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))
	request := func(path, token string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		return response.Code
	}
	if status := request("/agent/v1/grants", "tiny-access"); status != http.StatusOK {
		t.Fatalf("tiny token on agent route = %d", status)
	}
	if status := request("/agent/v1/grants", rawBroker); status != http.StatusUnauthorized {
		t.Fatalf("broker token on agent route = %d", status)
	}
	if status := request("/v1/models", rawBroker); status != http.StatusOK {
		t.Fatalf("broker token on inference route = %d", status)
	}
	if status := request("/v1/models", "tiny-access"); status != http.StatusUnauthorized {
		t.Fatalf("tiny token on inference route = %d", status)
	}
}

func TestListGrantsPropagatesAuthenticationClass(t *testing.T) {
	st := memory.New()
	for _, failure := range []*oidcauth.Failure{{Status: http.StatusUnauthorized, Code: "invalid_token"}, {Status: http.StatusForbidden, Code: "insufficient_scope"}, {Status: http.StatusServiceUnavailable, Code: "identity_service_unavailable"}} {
		auth := &fakeAuth{failure: failure}
		server, _ := New(st, auth)
		mux := http.NewServeMux()
		server.Register(mux)
		request := httptest.NewRequest(http.MethodGet, "/agent/v1/grants", nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != failure.Status || !strings.Contains(response.Body.String(), failure.Code) {
			t.Fatalf("failure %+v => %d %s", failure, response.Code, response.Body.String())
		}
	}
}
