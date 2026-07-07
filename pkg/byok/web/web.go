package web

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

//go:embed static
var staticFS embed.FS

// Config assembles the control plane.
type Config struct {
	Store         store.Store
	Vault         *vault.Vault
	SessionSecret string
	SessionMaxAge time.Duration
	OIDC          *OIDCConfig // nil disables OIDC login
	// DevUser enables a passwordless /dev-login route for local development.
	// NEVER set in production.
	DevUser string
}

type Server struct {
	store     store.Store
	vault     *vault.Vault
	sessions  *SessionCodec
	oidc      *oidcClient
	publicURL *url.URL
	devUser   string
}

func NewServer(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.Store == nil || cfg.Vault == nil {
		return nil, errors.New("byok/web: store and vault are required")
	}
	codec, err := NewSessionCodec(cfg.SessionSecret, cfg.SessionMaxAge)
	if err != nil {
		return nil, err
	}
	s := &Server{store: cfg.Store, vault: cfg.Vault, sessions: codec, devUser: cfg.DevUser}
	if cfg.OIDC != nil {
		client, err := newOIDCClient(ctx, *cfg.OIDC)
		if err != nil {
			return nil, err
		}
		s.oidc = client
		if parsed, err := url.Parse(cfg.OIDC.PublicURL); err == nil {
			s.publicURL = parsed
		}
	}
	if s.oidc == nil && s.devUser == "" {
		return nil, errors.New("byok/web: configure OIDC (--byok-oidc-issuer-url) or a dev user (--byok-dev-user)")
	}
	if s.devUser != "" {
		log.Warn().Str("dev_user", s.devUser).Msg("byok/web: DEV LOGIN ENABLED — do not use in production")
	}
	return s, nil
}

// Register mounts the control plane on mux. Data-plane routes (/v1/*,
// /healthz) are registered elsewhere; patterns here are more specific than
// the data plane's catch-all mount, so they take precedence.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)
	mux.HandleFunc("POST /logout", s.handleLogout)
	if s.devUser != "" {
		mux.HandleFunc("GET /dev-login", s.handleDevLogin)
	}

	mux.HandleFunc("GET /app", s.handleApp)
	staticContent, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))
	}

	mux.HandleFunc("GET /api/me", s.requireSession(s.handleMe))
	mux.HandleFunc("GET /api/credentials", s.requireSession(s.handleListCredentials))
	mux.HandleFunc("POST /api/credentials", s.requireSession(s.handleCreateCredential))
	mux.HandleFunc("DELETE /api/credentials/{id}", s.requireSession(s.handleDeleteCredential))
	mux.HandleFunc("GET /api/tokens", s.requireSession(s.handleListTokens))
	mux.HandleFunc("POST /api/tokens", s.requireSession(s.handleMintToken))
	mux.HandleFunc("POST /api/tokens/{id}/revoke", s.requireSession(s.handleRevokeToken))
	mux.HandleFunc("GET /api/usage", s.requireSession(s.handleUsage))
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if _, err := s.sessions.ClaimsFromRequest(r); err != nil {
		target := "/login?return_to=/app"
		if s.oidc == nil && s.devUser != "" {
			target = "/dev-login"
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	page, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "missing UI assets", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, claims SessionClaims) {
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id": claims.UserID, "username": claims.Username, "email": claims.Email,
	})
}
