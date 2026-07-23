// Package deviceclient implements the RFC 8628 coding-agent login and exchange
// into a broker-owned llmp capability. Provider credentials never cross this
// client boundary.
package deviceclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/byok/urlpolicy"
	"github.com/pkg/errors"
)

const maxBody = 64 << 10

type Config struct {
	IssuerURL  string
	ClientID   string
	Audience   string
	BrokerURL  string
	HTTPClient *http.Client
}

type Prompt struct {
	VerificationURI         string
	VerificationURIComplete string
	UserCode                string
	ExpiresAt               time.Time
}

type Grant struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	AllowedModels []string `json:"allowed_models"`
}

type Credential struct {
	Token            string    `json:"token"`
	TokenType        string    `json:"token_type"`
	BrokerURL        string    `json:"broker_url"`
	GrantID          string    `json:"grant_id"`
	AllowedModels    []string  `json:"allowed_models"`
	ExpiresAt        time.Time `json:"expires_at"`
	ClientInstanceID string    `json:"client_instance_id"`
}

type discovery struct {
	Issuer                      string `json:"issuer"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type Client struct {
	config    Config
	discovery discovery
	http      *http.Client
	sleep     func(context.Context, time.Duration) error
	now       func() time.Time
}

func New(ctx context.Context, config Config) (*Client, error) {
	if config.IssuerURL == "" || strings.TrimSpace(config.ClientID) == "" || config.Audience == "" || config.BrokerURL == "" {
		return nil, errors.New("device client issuer, client ID, audience, and broker URL are required")
	}
	issuerURL, err := urlpolicy.NormalizeSecure(config.IssuerURL, true)
	if err != nil {
		return nil, errors.Wrap(err, "invalid device issuer URL")
	}
	brokerURL, err := urlpolicy.NormalizeSecure(config.BrokerURL, true)
	if err != nil {
		return nil, errors.Wrap(err, "invalid broker URL")
	}
	if _, err := urlpolicy.NormalizeSecure(config.Audience, false); err != nil {
		return nil, errors.Wrap(err, "invalid agent audience URL")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}
	config.IssuerURL = issuerURL
	config.BrokerURL = brokerURL
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.IssuerURL+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	response, err := config.HTTPClient.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "device OIDC discovery")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.Errorf("device OIDC discovery returned %d", response.StatusCode)
	}
	var metadata discovery
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBody)).Decode(&metadata); err != nil {
		return nil, errors.Wrap(err, "decode device OIDC discovery")
	}
	if metadata.Issuer != config.IssuerURL {
		return nil, errors.New("device OIDC discovery issuer mismatch")
	}
	if err := validateIssuerEndpoint(config.IssuerURL, metadata.DeviceAuthorizationEndpoint); err != nil {
		return nil, errors.Wrap(err, "invalid device authorization endpoint")
	}
	if err := validateIssuerEndpoint(config.IssuerURL, metadata.TokenEndpoint); err != nil {
		return nil, errors.Wrap(err, "invalid token endpoint")
	}
	return &Client{config: config, discovery: metadata, http: config.HTTPClient, now: time.Now, sleep: sleepContext}, nil
}

func validateIssuerEndpoint(issuer, endpoint string) error {
	issuerURL, issuerErr := url.Parse(issuer)
	endpointURL, endpointErr := url.Parse(endpoint)
	if issuerErr != nil || endpointErr != nil || issuerURL.Scheme != endpointURL.Scheme || issuerURL.Host != endpointURL.Host || endpointURL.User != nil || endpointURL.RawQuery != "" || endpointURL.Fragment != "" {
		return errors.New("endpoint must be a clean issuer-origin URL")
	}
	if !strings.HasPrefix(endpointURL.Path, strings.TrimRight(issuerURL.Path, "/")+"/") {
		return errors.New("endpoint must be under issuer path")
	}
	return nil
}

func (c *Client) Login(ctx context.Context, clientInstanceID, requestedGrantID string, prompt func(Prompt) error) (Credential, error) {
	accessToken, err := c.deviceAuthorization(ctx, prompt)
	if err != nil {
		return Credential{}, err
	}
	grants, err := c.listGrants(ctx, accessToken)
	if err != nil {
		return Credential{}, err
	}
	grant, err := chooseGrant(grants, requestedGrantID)
	if err != nil {
		return Credential{}, err
	}
	credential, err := c.exchange(ctx, accessToken, clientInstanceID, grant.ID)
	if err != nil {
		return Credential{}, err
	}
	if len(credential.AllowedModels) == 0 {
		credential.AllowedModels = append([]string(nil), grant.AllowedModels...)
	}
	return credential, nil
}

func (c *Client) deviceAuthorization(ctx context.Context, prompt func(Prompt) error) (string, error) {
	form := url.Values{"client_id": {c.config.ClientID}, "scope": {"openid llm.tokens.issue"}, "resource": {c.config.Audience}}
	var response struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int64  `json:"expires_in"`
		Interval                int64  `json:"interval"`
	}
	if err := c.postForm(ctx, c.discovery.DeviceAuthorizationEndpoint, form, &response); err != nil {
		return "", err
	}
	if response.DeviceCode == "" || response.UserCode == "" || response.VerificationURI == "" || response.ExpiresIn <= 0 {
		return "", errors.New("device authorization response is incomplete")
	}
	expiresAt := c.now().UTC().Add(time.Duration(response.ExpiresIn) * time.Second)
	if prompt != nil {
		if err := prompt(Prompt{VerificationURI: response.VerificationURI, VerificationURIComplete: response.VerificationURIComplete, UserCode: response.UserCode, ExpiresAt: expiresAt}); err != nil {
			return "", err
		}
	}
	interval := time.Duration(response.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	for c.now().Before(expiresAt) {
		if err := c.sleep(ctx, interval); err != nil {
			return "", err
		}
		form = url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {response.DeviceCode}, "client_id": {c.config.ClientID}}
		var tokenResponse struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		status, err := c.postFormStatus(ctx, c.discovery.TokenEndpoint, form, &tokenResponse)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK && tokenResponse.AccessToken != "" {
			return tokenResponse.AccessToken, nil
		}
		switch tokenResponse.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
		case "access_denied":
			return "", errors.New("device authorization denied")
		case "expired_token":
			return "", errors.New("device authorization expired")
		default:
			return "", errors.New("device token exchange failed")
		}
	}
	return "", errors.New("device authorization expired")
}

func (c *Client) listGrants(ctx context.Context, accessToken string) ([]Grant, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BrokerURL+"/agent/v1/grants", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.http.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "list agent grants")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.Errorf("list agent grants returned %d", response.StatusCode)
	}
	var body struct {
		Grants []Grant `json:"grants"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBody)).Decode(&body); err != nil {
		return nil, errors.Wrap(err, "decode agent grants")
	}
	return body.Grants, nil
}

func chooseGrant(grants []Grant, requested string) (Grant, error) {
	if requested != "" {
		for _, grant := range grants {
			if grant.ID == requested {
				return grant, nil
			}
		}
		return Grant{}, errors.New("requested agent grant is unavailable")
	}
	if len(grants) == 1 {
		return grants[0], nil
	}
	if len(grants) == 0 {
		return Grant{}, errors.New("no eligible agent grants")
	}
	return Grant{}, errors.New("multiple agent grants are eligible; select one explicitly")
}

func (c *Client) exchange(ctx context.Context, accessToken, clientInstanceID, grantID string) (Credential, error) {
	body := strings.NewReader(fmt.Sprintf(`{"grant_id":%q,"client_instance_id":%q}`, grantID, clientInstanceID))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BrokerURL+"/agent/v1/tokens", body)
	if err != nil {
		return Credential{}, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return Credential{}, errors.Wrap(err, "exchange agent capability")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return Credential{}, errors.Errorf("agent capability exchange returned %d", response.StatusCode)
	}
	var credential Credential
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBody)).Decode(&credential); err != nil {
		return Credential{}, errors.Wrap(err, "decode agent capability")
	}
	if !strings.HasPrefix(credential.Token, "llmp_") || !strings.EqualFold(credential.TokenType, "Bearer") || credential.GrantID != grantID || credential.ExpiresAt.IsZero() {
		return Credential{}, errors.New("agent capability response is invalid")
	}
	credential.BrokerURL, credential.ClientInstanceID = c.config.BrokerURL, clientInstanceID
	return credential, nil
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, output any) error {
	status, err := c.postFormStatus(ctx, endpoint, form, output)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errors.Errorf("device endpoint returned %d", status)
	}
	return nil
}

func (c *Client) postFormStatus(ctx context.Context, endpoint string, form url.Values, output any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.http.Do(request)
	if err != nil {
		return 0, errors.Wrap(err, "device endpoint unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBody)).Decode(output); err != nil {
		return response.StatusCode, errors.Wrap(err, "decode device endpoint response")
	}
	return response.StatusCode, nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}
