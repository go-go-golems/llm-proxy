// Package oidcauth validates tiny-idp (or another compliant issuer) opaque
// access tokens through RFC 7662. Raw access tokens are never retained as map
// keys or included in errors.
package oidcauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/urlpolicy"
	"github.com/pkg/errors"
)

const maxResponseBody = 64 << 10

type Config struct {
	IssuerURL        string
	ResourceClientID string
	ClientSecret     []byte
	Audience         string
	AllowedClients   []string
	HTTPClient       *http.Client
	PositiveCacheTTL time.Duration
	NegativeCacheTTL time.Duration
}

type Principal struct {
	Issuer    string
	Subject   string
	ClientID  string
	Scopes    []string
	ExpiresAt time.Time
}

type Failure struct {
	Status int
	Code   string
}

func (f *Failure) Error() string { return f.Code }

type discoveryDocument struct {
	Issuer                   string   `json:"issuer"`
	IntrospectionEndpoint    string   `json:"introspection_endpoint"`
	IntrospectionAuthMethods []string `json:"introspection_endpoint_auth_methods_supported"`
}

type introspectionResponse struct {
	Active    bool            `json:"active"`
	Issuer    string          `json:"iss"`
	Subject   string          `json:"sub"`
	ClientID  string          `json:"client_id"`
	Scope     string          `json:"scope"`
	Audience  json.RawMessage `json:"aud"`
	Expires   int64           `json:"exp"`
	TokenType string          `json:"token_type"`
}

type cacheEntry struct {
	principal *Principal
	failure   *Failure
	expiresAt time.Time
}

type Authenticator struct {
	issuerURL        string
	endpoint         string
	resourceClientID string
	clientSecret     []byte
	audience         string
	allowedClients   map[string]struct{}
	httpClient       *http.Client
	positiveTTL      time.Duration
	negativeTTL      time.Duration
	cacheKey         [32]byte
	mu               sync.Mutex
	cache            map[string]cacheEntry
	now              func() time.Time
}

func New(ctx context.Context, config Config) (*Authenticator, error) {
	if strings.TrimSpace(config.IssuerURL) == "" || config.ResourceClientID == "" || len(config.ClientSecret) == 0 || config.Audience == "" || len(config.AllowedClients) == 0 {
		return nil, errors.New("OIDC introspection issuer, resource client, secret, audience, and allowed clients are required")
	}
	issuer, err := urlpolicy.NormalizeSecure(config.IssuerURL, true)
	if err != nil {
		return nil, errors.Wrap(err, "invalid OIDC introspection issuer")
	}
	if _, err := urlpolicy.NormalizeSecure(config.Audience, false); err != nil {
		return nil, errors.Wrap(err, "invalid OIDC introspection audience")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	if config.PositiveCacheTTL < 0 || config.PositiveCacheTTL > 5*time.Second || config.NegativeCacheTTL < 0 || config.NegativeCacheTTL > 5*time.Second {
		return nil, errors.New("OIDC introspection cache TTLs must be between zero and five seconds")
	}
	discoveryURL := issuer + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "construct OIDC discovery request")
	}
	response, err := config.HTTPClient.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "OIDC discovery unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.Errorf("OIDC discovery returned status %d", response.StatusCode)
	}
	var discovery discoveryDocument
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody)).Decode(&discovery); err != nil {
		return nil, errors.Wrap(err, "decode OIDC discovery")
	}
	if discovery.Issuer != issuer {
		return nil, errors.New("OIDC discovery issuer mismatch")
	}
	if !slices.Contains(discovery.IntrospectionAuthMethods, "client_secret_basic") {
		return nil, errors.New("OIDC issuer does not advertise client_secret_basic introspection")
	}
	if err := validateEndpoint(issuer, discovery.IntrospectionEndpoint); err != nil {
		return nil, err
	}
	authenticator := &Authenticator{
		issuerURL: issuer, endpoint: discovery.IntrospectionEndpoint,
		resourceClientID: config.ResourceClientID, clientSecret: append([]byte(nil), config.ClientSecret...),
		audience: config.Audience, allowedClients: make(map[string]struct{}, len(config.AllowedClients)),
		httpClient: config.HTTPClient, positiveTTL: config.PositiveCacheTTL, negativeTTL: config.NegativeCacheTTL,
		cache: make(map[string]cacheEntry), now: time.Now,
	}
	for _, clientID := range config.AllowedClients {
		if strings.TrimSpace(clientID) == "" {
			return nil, errors.New("OIDC allowed client IDs must be non-empty")
		}
		authenticator.allowedClients[clientID] = struct{}{}
	}
	if _, err := rand.Read(authenticator.cacheKey[:]); err != nil {
		return nil, errors.Wrap(err, "generate introspection cache key")
	}
	return authenticator, nil
}

func validateEndpoint(issuer, endpoint string) error {
	issuerURL, issuerErr := url.Parse(issuer)
	endpointURL, endpointErr := url.Parse(endpoint)
	if issuerErr != nil || endpointErr != nil || endpointURL.Scheme != issuerURL.Scheme || endpointURL.Host != issuerURL.Host || endpointURL.User != nil || endpointURL.RawQuery != "" || endpointURL.Fragment != "" {
		return errors.New("OIDC introspection endpoint must be a clean URL on the issuer origin")
	}
	issuerPath := strings.TrimRight(issuerURL.Path, "/") + "/"
	if !strings.HasPrefix(endpointURL.Path, issuerPath) {
		return errors.New("OIDC introspection endpoint must be under the issuer path")
	}
	return nil
}

func (a *Authenticator) Authenticate(ctx context.Context, authorizationHeaders []string, requiredScopes []string) (Principal, *Failure) {
	token, failure := exactBearer(authorizationHeaders)
	if failure != nil {
		return Principal{}, failure
	}
	key := a.tokenCacheKey(token)
	if entry, ok := a.cached(key); ok {
		if entry.failure != nil {
			return Principal{}, entry.failure
		}
		if failure := requireScopes(*entry.principal, requiredScopes); failure != nil {
			return Principal{}, failure
		}
		return *entry.principal, nil
	}
	principal, failure := a.introspect(ctx, token)
	if failure != nil {
		if failure.Status == http.StatusUnauthorized && a.negativeTTL > 0 {
			a.putCache(key, cacheEntry{failure: failure, expiresAt: a.now().Add(a.negativeTTL)})
		}
		return Principal{}, failure
	}
	if a.positiveTTL > 0 {
		expires := a.now().Add(a.positiveTTL)
		if principal.ExpiresAt.Before(expires) {
			expires = principal.ExpiresAt
		}
		cachedPrincipal := principal
		a.putCache(key, cacheEntry{principal: &cachedPrincipal, expiresAt: expires})
	}
	if failure := requireScopes(principal, requiredScopes); failure != nil {
		return Principal{}, failure
	}
	return principal, nil
}

func exactBearer(headers []string) (string, *Failure) {
	if len(headers) != 1 {
		return "", invalidToken()
	}
	parts := strings.Fields(headers[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", invalidToken()
	}
	return parts[1], nil
}

func (a *Authenticator) introspect(ctx context.Context, token string) (Principal, *Failure) {
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Principal{}, unavailable()
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// RFC 6749 section 2.3.1 applies application/x-www-form-urlencoded
	// encoding to both client credentials before HTTP Basic encoding.
	request.SetBasicAuth(url.QueryEscape(a.resourceClientID), url.QueryEscape(string(a.clientSecret)))
	response, err := a.httpClient.Do(request)
	if err != nil {
		return Principal{}, unavailable()
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBody))
		return Principal{}, unavailable()
	}
	var body introspectionResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBody))
	if err := decoder.Decode(&body); err != nil {
		return Principal{}, unavailable()
	}
	if !body.Active {
		return Principal{}, invalidToken()
	}
	now := a.now().UTC()
	expires := time.Unix(body.Expires, 0).UTC()
	audiences, ok := decodeAudience(body.Audience)
	_, clientAllowed := a.allowedClients[body.ClientID]
	if body.Issuer != a.issuerURL || body.Subject == "" || !clientAllowed || !ok || !slices.Contains(audiences, a.audience) || !strings.EqualFold(body.TokenType, "Bearer") || body.Expires <= 0 || !expires.After(now) {
		return Principal{}, invalidToken()
	}
	return Principal{Issuer: body.Issuer, Subject: body.Subject, ClientID: body.ClientID, Scopes: strings.Fields(body.Scope), ExpiresAt: expires}, nil
}

func decodeAudience(raw json.RawMessage) ([]string, bool) {
	var multiple []string
	if err := json.Unmarshal(raw, &multiple); err == nil {
		return multiple, len(multiple) > 0
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}, true
	}
	return nil, false
}

func requireScopes(principal Principal, required []string) *Failure {
	for _, scope := range required {
		if !slices.Contains(principal.Scopes, scope) {
			return &Failure{Status: http.StatusForbidden, Code: "insufficient_scope"}
		}
	}
	return nil
}

func invalidToken() *Failure { return &Failure{Status: http.StatusUnauthorized, Code: "invalid_token"} }
func unavailable() *Failure {
	return &Failure{Status: http.StatusServiceUnavailable, Code: "identity_service_unavailable"}
}

func (a *Authenticator) tokenCacheKey(token string) string {
	mac := hmac.New(sha256.New, a.cacheKey[:])
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) cached(key string) (cacheEntry, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[key]
	if !ok || !a.now().Before(entry.expiresAt) {
		delete(a.cache, key)
		return cacheEntry{}, false
	}
	return entry, true
}

func (a *Authenticator) putCache(key string, entry cacheEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= 1024 {
		for existingKey, existing := range a.cache {
			if !a.now().Before(existing.expiresAt) {
				delete(a.cache, existingKey)
			}
		}
	}
	if len(a.cache) < 1024 {
		a.cache[key] = entry
	}
}
