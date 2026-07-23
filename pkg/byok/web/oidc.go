package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

const (
	authTransactionCookie = "llm_proxy_auth_transaction"
	authCookieAge         = 10 * time.Minute
	devIdentityIssuer     = "urn:llm-proxy:dev"
)

// OIDCConfig configures the relying-party flow against tiny-idp (or any
// OIDC issuer with discovery).
type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	PublicURL    string // externally visible base URL of llm-proxy
}

type oidcClient struct {
	provider           *gooidc.Provider
	config             oauth2.Config
	verifier           *gooidc.IDTokenVerifier
	issuer             string
	endSessionEndpoint string
	postLogoutRedirect string
}

func newOIDCClient(ctx context.Context, cfg OIDCConfig) (*oidcClient, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, errors.Wrap(err, "OIDC discovery")
	}
	var metadata struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, errors.Wrap(err, "decode OIDC discovery metadata")
	}
	issuer := strings.TrimRight(cfg.IssuerURL, "/")
	if metadata.EndSessionEndpoint != "" {
		issuerURL, issuerErr := url.Parse(issuer)
		endpointURL, endpointErr := url.Parse(metadata.EndSessionEndpoint)
		if issuerErr != nil || endpointErr != nil {
			return nil, errors.New("OIDC end-session endpoint must be a clean URL under the issuer origin")
		}
		issuerPath := strings.TrimRight(issuerURL.Path, "/") + "/"
		if endpointURL.Scheme != issuerURL.Scheme || endpointURL.Host != issuerURL.Host || !strings.HasPrefix(endpointURL.Path, issuerPath) || endpointURL.RawQuery != "" || endpointURL.Fragment != "" || endpointURL.User != nil {
			return nil, errors.New("OIDC end-session endpoint must be a clean URL under the issuer origin")
		}
	}
	return &oidcClient{
		provider: provider,
		config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  strings.TrimRight(cfg.PublicURL, "/") + "/auth/callback",
			Scopes:       []string{gooidc.ScopeOpenID, "profile", "email"},
		},
		verifier:           provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		issuer:             issuer,
		endSessionEndpoint: metadata.EndSessionEndpoint,
		postLogoutRedirect: strings.TrimRight(cfg.PublicURL, "/") + "/",
	}, nil
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.Wrap(err, "generate random token")
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func setShortCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	// #nosec G124 -- Secure is derived from the inbound scheme so local HTTP
	// development continues to work; production deployments must serve the
	// public control-plane URL over HTTPS.
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), Expires: time.Now().Add(authCookieAge),
	})
}

func clearShortCookie(w http.ResponseWriter, r *http.Request, name string) {
	// #nosec G124 -- clearing mirrors the cookie attributes used when setting
	// the short-lived OIDC cookie, including scheme-derived Secure.
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

// sanitizeReturnTo accepts only local absolute-path references. It rejects
// absolute URLs, protocol-relative URLs, and backslash variants that some
// clients/proxies normalize into protocol-relative redirects.
func sanitizeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || len(raw) > 1 && (raw[1] == '/' || raw[1] == '\\') || strings.Contains(raw, "\\") {
		return "/app"
	}
	if u, err := url.Parse(raw); err != nil || u.Host != "" || u.Scheme != "" || !strings.HasPrefix(u.Path, "/") {
		return "/app"
	}
	return raw
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Error(w, "OIDC login is not configured (set --byok-oidc-issuer-url)", http.StatusNotImplemented)
		return
	}
	browserID, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	now := time.Now().UTC()
	if err := s.store.CreateAuthTransaction(r.Context(), store.AuthTransaction{
		IDHash: hashOpaque(browserID), StateHash: hashOpaque(state), Nonce: nonce,
		PKCEVerifier: verifier, ReturnTo: sanitizeReturnTo(r.URL.Query().Get("return_to")),
		CreatedAt: now, ExpiresAt: now.Add(authCookieAge),
	}); err != nil {
		log.Error().Err(err).Msg("byok/web: create auth transaction failed")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setShortCookie(w, r, authTransactionCookie, browserID)
	http.Redirect(w, r, s.oidc.config.AuthCodeURL(state,
		gooidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Error(w, "OIDC login is not configured", http.StatusNotImplemented)
		return
	}
	defer clearShortCookie(w, r, authTransactionCookie)

	browserCookie, err := r.Cookie(authTransactionCookie)
	state := r.URL.Query().Get("state")
	if err != nil || browserCookie.Value == "" || state == "" {
		http.Error(w, "invalid or expired authorization transaction", http.StatusBadRequest)
		return
	}
	transaction, err := s.store.ConsumeAuthTransaction(r.Context(),
		hashOpaque(browserCookie.Value), hashOpaque(state), time.Now().UTC())
	if err != nil {
		http.Error(w, "invalid or expired authorization transaction", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "authorization code is missing", http.StatusBadRequest)
		return
	}
	token, err := s.oidc.config.Exchange(r.Context(), code, oauth2.VerifierOption(transaction.PKCEVerifier))
	if err != nil {
		log.Error().Err(err).Msg("byok/web: code exchange failed")
		http.Error(w, "code exchange failed", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in token response", http.StatusBadGateway)
		return
	}
	idToken, err := s.oidc.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Error().Err(err).Msg("byok/web: id token verification failed")
		http.Error(w, "invalid id token", http.StatusUnauthorized)
		return
	}
	if strings.TrimRight(idToken.Issuer, "/") != s.oidc.issuer {
		http.Error(w, "invalid token issuer", http.StatusUnauthorized)
		return
	}
	if idToken.Nonce != transaction.Nonce {
		http.Error(w, "nonce mismatch", http.StatusBadRequest)
		return
	}
	var idClaims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := idToken.Claims(&idClaims); err != nil {
		http.Error(w, "invalid claims", http.StatusBadGateway)
		return
	}
	username := idClaims.PreferredUsername
	if username == "" {
		username = idClaims.Name
	}
	if username == "" {
		username = idToken.Subject
	}
	user, err := s.store.UpsertUser(r.Context(), store.User{
		OIDCIssuer: s.oidc.issuer, OIDCSubject: idToken.Subject,
		Username: username, Email: idClaims.Email,
	})
	if err != nil {
		log.Error().Err(err).Msg("byok/web: user provisioning failed")
		http.Error(w, "user provisioning failed", http.StatusInternalServerError)
		return
	}
	if err := s.createBrowserSession(r.Context(), w, r, user); err != nil {
		log.Error().Err(err).Msg("byok/web: create browser session failed")
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	// #nosec G710 -- the stored value was restricted by sanitizeReturnTo.
	http.Redirect(w, r, transaction.ReturnTo, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return
	}
	if rawID, err := s.sessions.IDFromRequest(r); err == nil {
		if err := s.store.RevokeSession(r.Context(), hashOpaque(rawID), time.Now().UTC()); err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Error().Err(err).Msg("byok/web: revoke browser session failed")
			http.Error(w, "logout unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	ClearCookie(w, r)
	if s.oidc != nil && s.oidc.endSessionEndpoint != "" {
		endpoint, err := url.Parse(s.oidc.endSessionEndpoint)
		if err != nil {
			http.Error(w, "logout unavailable", http.StatusServiceUnavailable)
			return
		}
		query := endpoint.Query()
		query.Set("client_id", s.oidc.config.ClientID)
		query.Set("post_logout_redirect_uri", s.oidc.postLogoutRedirect)
		endpoint.RawQuery = query.Encode()
		http.Redirect(w, r, endpoint.String(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

// handleDevLogin creates a session without OIDC. Only mounted when
// DevUser is explicitly configured; never enable in production.
func (s *Server) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	user, err := s.store.UpsertUser(r.Context(), store.User{
		OIDCIssuer: devIdentityIssuer, OIDCSubject: s.devUser, Username: s.devUser,
	})
	if err != nil {
		http.Error(w, "user provisioning failed", http.StatusInternalServerError)
		return
	}
	if err := s.createBrowserSession(r.Context(), w, r, user); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/app", http.StatusFound)
}
