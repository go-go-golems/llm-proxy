// Package urlpolicy validates credential-bearing OAuth destinations before any
// credential or token is attached to a request.
package urlpolicy

import (
	"net"
	"net/url"
	"strings"

	"github.com/pkg/errors"
)

// NormalizeSecure accepts clean absolute HTTPS URLs. HTTP is accepted only for
// loopback development and protocol tests. User info, query, and fragments are
// rejected because they make credential destinations ambiguous.
func NormalizeSecure(raw string, trimTrailingSlash bool) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("URL must be an absolute clean origin URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	secureTransport := parsed.Scheme == "https" || parsed.Scheme == "http" && loopback
	if !secureTransport {
		return "", errors.New("URL must use HTTPS except on loopback")
	}
	if trimTrailingSlash {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String(), nil
}
