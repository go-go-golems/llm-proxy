// Package web is the BYOK control plane: OIDC login against Keycloak (or any
// OIDC issuer), an HMAC-signed session cookie, a JSON management API for
// credentials/tokens/usage, and a small dashboard UI. It shares the store
// with the data plane; the data plane's bearer tokens are minted here.
package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const SessionCookieName = "llm_proxy_session"

// SessionClaims is the signed payload of the control-plane session cookie.
type SessionClaims struct {
	Subject  string `json:"sub"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	UserID   string `json:"user_id"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// SessionCodec signs and verifies session cookies. Not a JWT: a single
// HMAC-SHA256 over a JSON payload, `base64url(payload).base64url(sig)`.
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

func (c *SessionCodec) sign(payload []byte) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *SessionCodec) Encode(claims SessionClaims) (string, error) {
	now := time.Now().UTC()
	claims.IssuedAt = now.Unix()
	claims.Expires = now.Add(c.maxAge).Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", errors.Wrap(err, "marshal session claims")
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + c.sign(payload), nil
}

func (c *SessionCodec) Decode(value string) (SessionClaims, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return SessionClaims{}, errors.New("malformed session cookie")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return SessionClaims{}, errors.Wrap(err, "decode session payload")
	}
	expected := c.sign(payload)
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return SessionClaims{}, errors.New("invalid session signature")
	}
	var claims SessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return SessionClaims{}, errors.Wrap(err, "unmarshal session claims")
	}
	if time.Now().UTC().Unix() >= claims.Expires {
		return SessionClaims{}, errors.New("session expired")
	}
	return claims, nil
}

// SetCookie writes the session cookie; Secure is derived from the request.
func (c *SessionCodec) SetCookie(w http.ResponseWriter, r *http.Request, claims SessionClaims) error {
	value, err := c.Encode(claims)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: value, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure:  isSecureRequest(r),
		Expires: time.Now().Add(c.maxAge),
	})
	return nil
}

func ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isSecureRequest(r), MaxAge: -1,
	})
}

// ClaimsFromRequest verifies the session cookie on a request.
func (c *SessionCodec) ClaimsFromRequest(r *http.Request) (SessionClaims, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return SessionClaims{}, errors.New("no session")
	}
	return c.Decode(cookie.Value)
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
