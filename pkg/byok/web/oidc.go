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
	stateCookie    = "llm_proxy_auth_state"
	nonceCookie    = "llm_proxy_auth_nonce"
	returnToCookie = "llm_proxy_auth_return_to"
	authCookieAge  = 10 * time.Minute
)

// OIDCConfig configures the relying-party flow against Keycloak (or any
// OIDC issuer with discovery).
type OIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	PublicURL    string // externally visible base URL of llm-proxy
}

type oidcClient struct {
	provider *gooidc.Provider
	config   oauth2.Config
	verifier *gooidc.IDTokenVerifier
}

func newOIDCClient(ctx context.Context, cfg OIDCConfig) (*oidcClient, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, errors.Wrap(err, "OIDC discovery")
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
		verifier: provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
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
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), Expires: time.Now().Add(authCookieAge),
	})
}

func clearShortCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), MaxAge: -1,
	})
}

// sanitizeReturnTo rejects absolute and protocol-relative URLs (open redirect).
func sanitizeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/app"
	}
	if u, err := url.Parse(raw); err != nil || u.Host != "" || u.Scheme != "" {
		return "/app"
	}
	return raw
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Error(w, "OIDC login is not configured (set --byok-oidc-issuer-url)", http.StatusNotImplemented)
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
	setShortCookie(w, r, stateCookie, state)
	setShortCookie(w, r, nonceCookie, nonce)
	setShortCookie(w, r, returnToCookie, sanitizeReturnTo(r.URL.Query().Get("return_to")))
	http.Redirect(w, r, s.oidc.config.AuthCodeURL(state, gooidc.Nonce(nonce)), http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		http.Error(w, "OIDC login is not configured", http.StatusNotImplemented)
		return
	}
	defer func() {
		clearShortCookie(w, r, stateCookie)
		clearShortCookie(w, r, nonceCookie)
		clearShortCookie(w, r, returnToCookie)
	}()

	stateC, err := r.Cookie(stateCookie)
	if err != nil || stateC.Value == "" || r.URL.Query().Get("state") != stateC.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	token, err := s.oidc.config.Exchange(r.Context(), r.URL.Query().Get("code"))
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
	nonceC, err := r.Cookie(nonceCookie)
	if err != nil || idToken.Nonce != nonceC.Value {
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
		OIDCSubject: idToken.Subject, Username: username, Email: idClaims.Email,
	})
	if err != nil {
		log.Error().Err(err).Msg("byok/web: user provisioning failed")
		http.Error(w, "user provisioning failed", http.StatusInternalServerError)
		return
	}
	if err := s.sessions.SetCookie(w, r, SessionClaims{
		Subject: idToken.Subject, Username: user.Username, Email: user.Email, UserID: user.ID,
	}); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	returnTo := "/app"
	if c, err := r.Cookie(returnToCookie); err == nil {
		returnTo = sanitizeReturnTo(c.Value)
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ClearCookie(w, r)
	http.Redirect(w, r, "/app", http.StatusFound)
}

// handleDevLogin creates a session without OIDC. Only mounted when
// DevUser is explicitly configured; never enable in production.
func (s *Server) handleDevLogin(w http.ResponseWriter, r *http.Request) {
	user, err := s.store.UpsertUser(r.Context(), store.User{
		OIDCSubject: "local:" + s.devUser, Username: s.devUser,
	})
	if err != nil {
		http.Error(w, "user provisioning failed", http.StatusInternalServerError)
		return
	}
	if err := s.sessions.SetCookie(w, r, SessionClaims{
		Subject: user.OIDCSubject, Username: user.Username, UserID: user.ID,
	}); err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/app", http.StatusFound)
}
