package authmw_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
	"github.com/go-go-golems/llm-proxy/pkg/server"
)

func setupToken(t *testing.T, st store.Store, mutate func(*store.Token)) (string, store.Token) {
	t.Helper()
	ctx := context.Background()
	u, err := st.UpsertUser(ctx, store.User{OIDCIssuer: "urn:test", OIDCSubject: "sub", Username: "alice"})
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	raw, hash, err := tokens.Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	tk := store.Token{
		UserID: u.ID, TokenHash: hash, Name: "test",
		CredentialIDs: []string{"c1"}, AllowedModels: []string{"sonnet", "gpt-*"},
	}
	if mutate != nil {
		mutate(&tk)
	}
	tok, err := st.MintToken(ctx, tk)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return raw, tok
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("passed"))
	})
}

func do(t *testing.T, h http.Handler, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestTokenAuthRejectsMissingAndInvalid(t *testing.T) {
	st := memory.New()
	h := authmw.TokenAuth(st, okHandler())

	if w := do(t, h, "/v1/models", ""); w.Code != 401 || !strings.Contains(w.Body.String(), "missing_api_key") {
		t.Fatalf("missing token: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "/v1/models", "llmp_bogus"); w.Code != 401 || !strings.Contains(w.Body.String(), "invalid_api_key") {
		t.Fatalf("invalid token: %d %s", w.Code, w.Body.String())
	}
	// Non-/v1 paths bypass enforcement.
	if w := do(t, h, "/healthz", ""); w.Code != 200 {
		t.Fatalf("healthz should pass: %d", w.Code)
	}
}

type unavailableMeterHealth struct{}

func (unavailableMeterHealth) BeforeInference(context.Context) error {
	return errors.New("meter unavailable")
}

func TestTokenAuthMeterCircuitPreventsProviderDispatch(t *testing.T) {
	st := memory.New()
	raw, _ := setupToken(t, st, nil)
	dispatched := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatched++
		w.WriteHeader(http.StatusOK)
	})
	h := authmw.TokenAuthWithMeterHealth(st, unavailableMeterHealth{}, inner)
	w := do(t, h, "/v1/chat/completions", raw)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "metering_unavailable") {
		t.Fatalf("open meter circuit response: %d %s", w.Code, w.Body.String())
	}
	if dispatched != 0 {
		t.Fatalf("provider path dispatched %d times while meter circuit was open", dispatched)
	}
	if w := do(t, h, "/healthz", ""); w.Code != http.StatusOK || dispatched != 1 {
		t.Fatalf("non-data-plane route did not bypass meter circuit: status=%d dispatched=%d", w.Code, dispatched)
	}
}

func TestTokenAuthAcceptsValidToken(t *testing.T) {
	st := memory.New()
	raw, tok := setupToken(t, st, nil)
	var seen store.Token
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = authmw.TokenFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := authmw.TokenAuth(st, inner)

	if w := do(t, h, "/v1/models", raw); w.Code != 200 {
		t.Fatalf("valid token rejected: %d %s", w.Code, w.Body.String())
	}
	if seen.ID != tok.ID {
		t.Fatalf("token not in context: %+v", seen)
	}
	got, err := st.GetTokenByHash(context.Background(), tok.TokenHash)
	if err != nil || got.LastUsedAt == nil {
		t.Fatalf("last_used_at not touched: %v %+v", err, got.LastUsedAt)
	}
}

func TestTokenAuthRevokedAndExpired(t *testing.T) {
	st := memory.New()
	rawRevoked, tokRevoked := setupToken(t, st, nil)
	if err := st.RevokeToken(context.Background(), tokRevoked.UserID, tokRevoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	rawExpired, _ := setupToken(t, st, func(tk *store.Token) {
		tk.ExpiresAt = &past
	})
	h := authmw.TokenAuth(st, okHandler())

	if w := do(t, h, "/v1/models", rawRevoked); w.Code != 401 || !strings.Contains(w.Body.String(), "token_revoked") {
		t.Fatalf("revoked: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "/v1/models", rawExpired); w.Code != 401 || !strings.Contains(w.Body.String(), "token_expired") {
		t.Fatalf("expired: %d %s", w.Code, w.Body.String())
	}
	// Rejections are audited.
	events, err := st.ListEvents(context.Background(), store.AuditFilter{EventType: "inference.rejected"})
	if err != nil || len(events) != 2 {
		t.Fatalf("audit events: %v (%d)", err, len(events))
	}
}

func TestTokenAuthBudgets(t *testing.T) {
	st := memory.New()
	budget := int64(100)
	raw, tok := setupToken(t, st, func(tk *store.Token) { tk.MaxTotalTokens = &budget })
	h := authmw.TokenAuth(st, okHandler())

	if w := do(t, h, "/v1/chat/completions", raw); w.Code != 200 {
		t.Fatalf("under budget rejected: %d", w.Code)
	}
	if err := st.RecordUsage(context.Background(), store.LedgerEntry{
		TokenID: tok.ID, UserID: tok.UserID, Model: "sonnet",
		PromptTokens: 80, CompletionTokens: 25, Status: store.LedgerStatusOK,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if w := do(t, h, "/v1/chat/completions", raw); w.Code != 429 || !strings.Contains(w.Body.String(), "budget_exhausted") {
		t.Fatalf("over budget: %d %s", w.Code, w.Body.String())
	}
}

func TestAgentGrantCumulativeBudgetAcrossRotatedTokens(t *testing.T) {
	st := memory.New()
	ctx := context.Background()
	user, err := st.UpsertUser(ctx, store.User{OIDCIssuer: "urn:test", OIDCSubject: "grant-user", Username: "grant-user"})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := st.CreateCredential(ctx, store.Credential{UserID: user.ID, Provider: "openai", APIType: "openai", Label: "agent", SecretCipher: []byte{1}, SecretLast4: "safe"})
	if err != nil {
		t.Fatal(err)
	}
	maxRequests := int64(1)
	grant, err := st.CreateAgentGrantAudited(ctx, store.AgentGrant{UserID: user.ID, Name: "laptop", CredentialIDs: []string{credential.ID}, AllowedModels: []string{"sonnet"}, TokenTTL: time.Hour, MaxActivePerInstance: 1, GrantMaxRequests: &maxRequests})
	if err != nil {
		t.Fatal(err)
	}
	mint := func() (string, store.Token) {
		raw, hash, err := tokens.Mint()
		if err != nil {
			t.Fatal(err)
		}
		expires := time.Now().UTC().Add(grant.TokenTTL)
		token, err := st.MintToken(ctx, store.Token{UserID: user.ID, TokenHash: hash, Name: "device", CredentialIDs: grant.CredentialIDs, AllowedModels: grant.AllowedModels, AgentGrantID: grant.ID, IssueChannel: store.IssueChannelDevice, ExpiresAt: &expires})
		if err != nil {
			t.Fatal(err)
		}
		return raw, token
	}
	raw1, _ := mint()
	raw2, _ := mint()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _ := authmw.TokenFrom(r.Context())
		if err := st.RecordUsage(r.Context(), store.LedgerEntry{TokenID: token.ID, UserID: token.UserID, Model: "sonnet", Status: store.LedgerStatusOK}); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	})
	h := authmw.TokenAuth(st, inner)
	if w := do(t, h, "/v1/chat/completions", raw1); w.Code != http.StatusOK {
		t.Fatalf("first grant request = %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "/v1/chat/completions", raw2); w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "budget_exhausted") {
		t.Fatalf("rotated token reset grant budget: %d %s", w.Code, w.Body.String())
	}
}

func TestTokenAuthRequestBudgetAndRateLimit(t *testing.T) {
	st := memory.New()
	maxReq := int64(1)
	rawReq, tokReq := setupToken(t, st, func(tk *store.Token) { tk.MaxRequests = &maxReq })
	h := authmw.TokenAuth(st, okHandler())

	if w := do(t, h, "/v1/completions", rawReq); w.Code != 200 {
		t.Fatalf("first request rejected: %d", w.Code)
	}
	if err := st.RecordUsage(context.Background(), store.LedgerEntry{
		TokenID: tokReq.ID, UserID: tokReq.UserID, Model: "sonnet", Status: store.LedgerStatusOK,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if w := do(t, h, "/v1/completions", rawReq); w.Code != 429 {
		t.Fatalf("request budget not enforced: %d %s", w.Code, w.Body.String())
	}

	rpm := int64(2)
	rawRPM, _ := setupToken(t, st, func(tk *store.Token) {
		tk.TokenHash = tokens.Hash("llmp_other")
		tk.RateLimitRPM = &rpm
	})
	_ = rawRPM // the mutate above replaced the hash; use the known plaintext
	for i := 0; i < 2; i++ {
		if w := do(t, h, "/v1/completions", "llmp_other"); w.Code != 200 {
			t.Fatalf("rpm request %d rejected: %d", i, w.Code)
		}
	}
	if w := do(t, h, "/v1/completions", "llmp_other"); w.Code != 429 || !strings.Contains(w.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("rate limit not enforced: %d %s", w.Code, w.Body.String())
	}
}

func TestTokenAuthSerializesRequestBudgetThroughDispatch(t *testing.T) {
	st := memory.New()
	maxReq := int64(1)
	raw, _ := setupToken(t, st, func(tk *store.Token) { tk.MaxRequests = &maxReq })

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		tok, ok := authmw.TokenFrom(r.Context())
		if !ok {
			t.Errorf("token missing from context")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := st.RecordUsage(r.Context(), store.LedgerEntry{
			TokenID: tok.ID, UserID: tok.UserID, Model: "sonnet", Status: store.LedgerStatusOK,
		}); err != nil {
			t.Errorf("record usage: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := authmw.TokenAuth(st, inner)

	type result struct{ code int }
	results := make(chan result, 2)
	run := func() {
		w := do(t, h, "/v1/completions", raw)
		results <- result{code: w.Code}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); run() }()
	<-entered
	go func() { defer wg.Done(); run() }()
	time.Sleep(25 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)

	var okCount, rejectedCount int
	for r := range results {
		switch r.code {
		case http.StatusOK:
			okCount++
		case http.StatusTooManyRequests:
			rejectedCount++
		default:
			t.Fatalf("unexpected status %d", r.code)
		}
	}
	if okCount != 1 || rejectedCount != 1 {
		t.Fatalf("expected one accepted request and one budget rejection, got ok=%d rejected=%d", okCount, rejectedCount)
	}
}

func TestScopedModelLister(t *testing.T) {
	inner := fixedLister{
		{ID: "sonnet", Object: "model", OwnedBy: "geppetto-profile"},
		{ID: "gpt-4o-mini", Object: "model", OwnedBy: "geppetto-profile"},
		{ID: "gemini-flash", Object: "model", OwnedBy: "geppetto-profile"},
	}
	lister := &authmw.ScopedModelLister{Inner: inner}

	// No token in context: fail closed.
	models, err := lister.ListModels(context.Background())
	if err != nil || len(models) != 0 {
		t.Fatalf("expected empty list without token: %v (%d)", err, len(models))
	}

	ctx := authmw.WithToken(context.Background(), store.Token{AllowedModels: []string{"sonnet", "gpt-*"}})
	models, err = lister.ListModels(ctx)
	if err != nil || len(models) != 2 {
		t.Fatalf("scoped list: %v %+v", err, models)
	}
	for _, m := range models {
		if m.ID == "gemini-flash" {
			t.Fatal("disallowed model leaked through scope")
		}
	}
}

type fixedLister []server.ModelDescriptor

func (l fixedLister) ListModels(context.Context) ([]server.ModelDescriptor, error) {
	return l, nil
}
