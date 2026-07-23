package deviceclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDeviceAuthorizationGrantExchange(t *testing.T) {
	var baseURL string
	var mu sync.Mutex
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": baseURL, "device_authorization_endpoint": baseURL + "/device_authorization", "token_endpoint": baseURL + "/token"})
		case "/device_authorization":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "agent" || r.Form.Get("resource") != "https://broker.example/agent/v1" || !strings.Contains(r.Form.Get("scope"), "llm.tokens.issue") {
				t.Error("invalid device request")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "device-secret", "user_code": "user-code", "verification_uri": baseURL + "/device", "verification_uri_complete": baseURL + "/device?user_code=user-code", "expires_in": 600, "interval": 1})
		case "/token":
			mu.Lock()
			polls++
			current := polls
			mu.Unlock()
			if current == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tiny-access", "token_type": "Bearer"})
		case "/agent/v1/grants":
			if r.Header.Get("Authorization") != "Bearer tiny-access" {
				t.Error("missing tiny access token")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"grants": []any{map[string]any{"id": "grant", "name": "laptop", "allowed_models": []string{"model"}}}})
		case "/agent/v1/tokens":
			if r.Header.Get("Authorization") != "Bearer tiny-access" {
				t.Error("missing exchange identity")
			}
			var request map[string]string
			_ = json.NewDecoder(r.Body).Decode(&request)
			if request["grant_id"] != "grant" || request["client_instance_id"] != "0123456789abcdef" {
				t.Errorf("exchange request = %#v", request)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "llmp_capability", "token_type": "Bearer", "grant_id": "grant", "allowed_models": []string{"model"}, "expires_at": time.Now().Add(time.Hour)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client, err := New(context.Background(), Config{IssuerURL: baseURL, ClientID: "agent", Audience: "https://broker.example/agent/v1", BrokerURL: baseURL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return nil }
	prompted := false
	credential, err := client.Login(context.Background(), "0123456789abcdef", "", func(prompt Prompt) error {
		prompted = prompt.UserCode != "" && prompt.VerificationURI != ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !prompted || credential.Token != "llmp_capability" || credential.GrantID != "grant" || credential.BrokerURL != baseURL {
		t.Fatalf("credential = %+v prompted=%v", credential, prompted)
	}
}

func TestDevicePollingSlowDownAndTerminalErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		errors    []string
		wantError string
		wantSleep []time.Duration
	}{
		{name: "slow down then denied", errors: []string{"slow_down", "access_denied"}, wantError: "denied", wantSleep: []time.Duration{time.Second, 6 * time.Second}},
		{name: "expired", errors: []string{"expired_token"}, wantError: "expired", wantSleep: []time.Duration{time.Second}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var issuer string
			poll := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "device_authorization_endpoint": issuer + "/device_authorization", "token_endpoint": issuer + "/token"})
				case "/device_authorization":
					_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "opaque-device-value", "user_code": "display-value", "verification_uri": issuer + "/device", "expires_in": 600, "interval": 1})
				case "/token":
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": test.errors[poll]})
					poll++
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			issuer = server.URL
			client, err := New(context.Background(), Config{IssuerURL: issuer, ClientID: "agent", Audience: "https://broker.example/agent/v1", BrokerURL: issuer, HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			var sleeps []time.Duration
			client.sleep = func(_ context.Context, duration time.Duration) error { sleeps = append(sleeps, duration); return nil }
			_, err = client.Login(context.Background(), "0123456789abcdef", "", nil)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v", err)
			}
			if len(sleeps) != len(test.wantSleep) {
				t.Fatalf("sleep count = %v", sleeps)
			}
			for index := range sleeps {
				if sleeps[index] != test.wantSleep[index] {
					t.Fatalf("sleeps = %v", sleeps)
				}
			}
		})
	}
}

func TestDeviceClientRejectsInsecureCredentialDestinations(t *testing.T) {
	for _, config := range []Config{
		{IssuerURL: "http://idp.example", ClientID: "agent", Audience: "https://broker.example/agent/v1", BrokerURL: "https://broker.example"},
		{IssuerURL: "https://idp.example", ClientID: "agent", Audience: "http://broker.example/agent/v1", BrokerURL: "https://broker.example"},
		{IssuerURL: "https://idp.example", ClientID: "agent", Audience: "https://broker.example/agent/v1", BrokerURL: "http://broker.example"},
		{IssuerURL: "https://idp.example", ClientID: "agent", Audience: "https://broker.example/agent/v1", BrokerURL: "https://user@broker.example"},
	} {
		if _, err := New(context.Background(), config); err == nil {
			t.Fatalf("accepted insecure config: %+v", config)
		}
	}
}

func TestDeviceDiscoveryRejectsCrossOriginEndpoints(t *testing.T) {
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "device_authorization_endpoint": "https://evil.example/device", "token_endpoint": issuer + "/token"})
	}))
	defer server.Close()
	issuer = server.URL
	if _, err := New(context.Background(), Config{IssuerURL: issuer, ClientID: "agent", Audience: "https://broker.example/agent/v1", BrokerURL: issuer, HTTPClient: server.Client()}); err == nil {
		t.Fatal("cross-origin endpoint accepted")
	}
}

func TestCredentialCachePermissionsLifecycleAndStableInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credential.json")
	instance, err := LoadOrCreateClientInstanceID(path)
	if err != nil || len(instance) != 32 {
		t.Fatalf("instance = %q, %v", instance, err)
	}
	beforeLoginRetry, err := LoadOrCreateClientInstanceID(path)
	if err != nil || beforeLoginRetry != instance {
		t.Fatalf("pre-login retry instance = %q, %v", beforeLoginRetry, err)
	}
	credential := Credential{Token: "llmp_secret", TokenType: "Bearer", BrokerURL: "https://broker.example", GrantID: "grant", ExpiresAt: time.Now().Add(time.Hour), ClientInstanceID: instance}
	if err := SaveCredential(path, credential); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode = %v, %v", info.Mode().Perm(), err)
	}
	loaded, err := LoadCredential(path)
	if err != nil || loaded.Token != credential.Token {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}
	stable, err := LoadOrCreateClientInstanceID(path)
	if err != nil || stable != instance {
		t.Fatalf("stable instance = %q, %v", stable, err)
	}
	if err := DeleteCredential(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredential(path); err != ErrCacheNotFound {
		t.Fatalf("load deleted = %v", err)
	}
}

func TestCredentialCacheRejectsUnsafeDirectoryAndLock(t *testing.T) {
	credential := Credential{Token: "llmp_test", ClientInstanceID: "0123456789abcdef"}
	t.Run("permissive directory", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "cache")
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "credential.json")
		if err := SaveCredential(path, credential); err == nil || !strings.Contains(err.Error(), "0700") {
			t.Fatalf("error = %v", err)
		}
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("directory mode was mutated to %o", info.Mode().Perm())
		}
	})
	t.Run("directory symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := SaveCredential(filepath.Join(link, "credential.json"), credential); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("permissive lock", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "cache")
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "credential.json")
		if err := os.WriteFile(path+".lock", nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path+".lock", 0o644); err != nil {
			t.Fatal(err)
		}
		if err := SaveCredential(path, credential); err == nil || !strings.Contains(err.Error(), "lock") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestCredentialCacheRejectsSymlink(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "credential.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCredential(path); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestChooseGrantRequiresExplicitChoiceWhenAmbiguous(t *testing.T) {
	grants := []Grant{{ID: "one"}, {ID: "two"}}
	if _, err := chooseGrant(grants, ""); err == nil {
		t.Fatal("ambiguous grants auto-selected")
	}
	chosen, err := chooseGrant(grants, "two")
	if err != nil || chosen.ID != "two" {
		t.Fatalf("chosen = %+v, %v", chosen, err)
	}
}
