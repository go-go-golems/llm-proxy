// Package web is the BYOK control plane: OIDC login against tiny-idp (or any
// compliant issuer), server-side browser sessions, credential/token management,
// and the dashboard UI.
package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

const SessionCookieName = "llm_proxy_session"

// SessionClaims is assembled from server-side session and user records after
// every successful cookie validation. It is never serialized into the cookie.
type SessionClaims struct {
	Subject       string
	Issuer        string
	Username      string
	Email         string
	UserID        string
	SessionIDHash string
}

// SessionCodec signs and verifies one opaque browser session identifier. The
// database stores only its SHA-256 hash.
type SessionCodec struct {
	secret []byte
	maxAge time.Duration
}

func NewSessionCodec(secret string, maxAge time.Duration) (*SessionCodec, error) {
	if len(secret) < 16 {
		return nil, errors.New("session secret must be at least 16 characters")
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	return &SessionCodec{secret: []byte(secret), maxAge: maxAge}, nil
}

func (c *SessionCodec) sign(sessionID string) string {
	mac := hmac.New(sha256.New, c.secret)
	_, _ = mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *SessionCodec) Encode(sessionID string) (string, error) {
	if sessionID == "" {
		return "", errors.New("session ID is required")
	}
	return sessionID + "." + c.sign(sessionID), nil
}

func (c *SessionCodec) Decode(value string) (string, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("malformed session cookie")
	}
	if !hmac.Equal([]byte(c.sign(parts[0])), []byte(parts[1])) {
		return "", errors.New("invalid session signature")
	}
	return parts[0], nil
}

func (c *SessionCodec) SetCookie(w http.ResponseWriter, r *http.Request, sessionID string) error {
	value, err := c.Encode(sessionID)
	if err != nil {
		return err
	}
	// #nosec G124 -- production requests arrive through the trusted HTTPS proxy;
	// local HTTP development remains intentionally possible.
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: value, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), Expires: time.Now().Add(c.maxAge),
	})
	return nil
}

func (c *SessionCodec) IDFromRequest(r *http.Request) (string, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return "", errors.New("no session")
	}
	return c.Decode(cookie.Value)
}

func ClearCookie(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- clearing mirrors the cookie attributes used when setting it.
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func hashOpaque(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (s *Server) createBrowserSession(ctx context.Context, w http.ResponseWriter, r *http.Request, user store.User) error {
	rawID, err := randomToken()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.store.CreateSession(ctx, store.Session{
		ID: store.NewID(), IDHash: hashOpaque(rawID), UserID: user.ID, CreatedAt: now,
		LastSeenAt: now, ExpiresAt: now.Add(s.sessionMaxAge),
	}); err != nil {
		return err
	}
	return s.sessions.SetCookie(w, r, rawID)
}

func (s *Server) claimsFromRequest(r *http.Request) (SessionClaims, error) {
	rawID, err := s.sessions.IDFromRequest(r)
	if err != nil {
		return SessionClaims{}, err
	}
	now := time.Now().UTC()
	session, err := s.store.UseSession(r.Context(), hashOpaque(rawID), now, now.Add(-s.sessionIdleTimeout))
	if err != nil {
		return SessionClaims{}, err
	}
	user, err := s.store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		return SessionClaims{}, err
	}
	return SessionClaims{
		Subject: user.OIDCSubject, Issuer: user.OIDCIssuer, Username: user.Username,
		Email: user.Email, UserID: user.ID, SessionIDHash: session.IDHash,
	}, nil
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
