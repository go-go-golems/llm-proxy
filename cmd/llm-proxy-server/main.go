package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	byokcmds "github.com/go-go-golems/llm-proxy/cmd/llm-proxy-server/cmds/byok"
	byokagentapi "github.com/go-go-golems/llm-proxy/pkg/byok/agentapi"
	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	byokengines "github.com/go-go-golems/llm-proxy/pkg/byok/engines"
	byokmeter "github.com/go-go-golems/llm-proxy/pkg/byok/meter"
	byokoidcauth "github.com/go-go-golems/llm-proxy/pkg/byok/oidcauth"
	byokstorepkg "github.com/go-go-golems/llm-proxy/pkg/byok/store"
	byoksqlite "github.com/go-go-golems/llm-proxy/pkg/byok/store/sqlite"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
	byokweb "github.com/go-go-golems/llm-proxy/pkg/byok/web"
	llmproxydoc "github.com/go-go-golems/llm-proxy/pkg/doc"
	profilespkg "github.com/go-go-golems/llm-proxy/pkg/profiles"
	runtimepkg "github.com/go-go-golems/llm-proxy/pkg/runtime"
	"github.com/go-go-golems/llm-proxy/pkg/server"
	"github.com/spf13/cobra"
)

type ServeCommand struct {
	*cmds.CommandDescription
}

type ServeSettings struct {
	Listen               string `glazed:"listen"`
	Profiles             string `glazed:"profiles"`
	ByokDB               string `glazed:"byok-db"`
	ByokLegacyOIDCIssuer string `glazed:"byok-legacy-oidc-issuer"`
	ByokMasterKey        string `glazed:"byok-master-key"`
	ByokMasterKeyFile    string `glazed:"byok-master-key-file"`

	ByokSessionSecret                  string `glazed:"byok-session-secret"`
	ByokSessionSecretFile              string `glazed:"byok-session-secret-file"`
	ByokSessionAbsoluteLifetime        string `glazed:"byok-session-absolute-lifetime"`
	ByokSessionIdleTimeout             string `glazed:"byok-session-idle-timeout"`
	ByokAgentMaxTokenTTL               string `glazed:"byok-agent-max-token-ttl"`
	ByokMeterTransientFailureThreshold int    `glazed:"byok-meter-transient-failure-threshold"`
	ByokMeterRecoveryCooldown          string `glazed:"byok-meter-recovery-cooldown"`
	ByokOIDCIssuerURL                  string `glazed:"byok-oidc-issuer-url"`
	ByokOIDCClientID                   string `glazed:"byok-oidc-client-id"`
	ByokOIDCClientSecret               string `glazed:"byok-oidc-client-secret"`
	ByokOIDCClientSecretFile           string `glazed:"byok-oidc-client-secret-file"`
	ByokOIDCResourceClientID           string `glazed:"byok-oidc-resource-client-id"`
	ByokOIDCResourceClientSecret       string `glazed:"byok-oidc-resource-client-secret"`
	ByokOIDCResourceClientSecretFile   string `glazed:"byok-oidc-resource-client-secret-file"`
	ByokAgentAudience                  string `glazed:"byok-agent-audience"`
	ByokAgentAllowedClients            string `glazed:"byok-agent-allowed-clients"`
	ByokIntrospectionPositiveCacheTTL  string `glazed:"byok-introspection-positive-cache-ttl"`
	ByokIntrospectionNegativeCacheTTL  string `glazed:"byok-introspection-negative-cache-ttl"`
	ByokPublicURL                      string `glazed:"byok-public-url"`
	ByokDevUser                        string `glazed:"byok-dev-user"`
}

func main() {
	rootCmd := newRootCommand()
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "llm-proxy-server",
		Short: "Serve an OpenAI-compatible HTTP API backed by Geppetto profiles",
		Long: `llm-proxy-server exposes OpenAI-compatible model, completion, and chat-completion endpoints.

Provider setup lives in Geppetto profile YAML. Without a profiles file the server still exposes health checks and static stub responses, which is useful for smoke tests and local wiring.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return logging.InitLoggerFromCobra(cmd)
		},
	}
	cobra.CheckErr(logging.AddLoggingSectionToRootCommand(rootCmd, "llm-proxy"))

	helpSystem := help.NewHelpSystem()
	cobra.CheckErr(llmproxydoc.AddDocToHelpSystem(helpSystem))
	help_cmd.SetupCobraRootCommand(helpSystem, rootCmd)

	serveCmd, err := NewServeCommand()
	cobra.CheckErr(err)
	serveCobraCmd, err := cli.BuildCobraCommandFromCommand(serveCmd,
		cli.WithParserConfig(cli.CobraParserConfig{
			ShortHelpSections: []string{schema.DefaultSlug},
			AppName:           byokcmds.AppName,
		}),
	)
	cobra.CheckErr(err)
	rootCmd.AddCommand(serveCobraCmd)

	byokCobraCmd, err := byokcmds.NewCommand()
	cobra.CheckErr(err)
	rootCmd.AddCommand(byokCobraCmd)

	return rootCmd
}

func NewServeCommand() (*ServeCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}

	cmdDesc := cmds.NewCommandDescription(
		"serve",
		cmds.WithShort("Serve the OpenAI-compatible LLM proxy HTTP API"),
		cmds.WithLong(`Serve the OpenAI-compatible LLM proxy HTTP API.

The server exposes /healthz, /readyz, /v1/models, /v1/completions, and /v1/chat/completions. Provider setup lives in Geppetto profile YAML; without a profiles file the server still exposes health checks and static stub responses.

Examples:

  llm-proxy-server serve
  llm-proxy-server serve --listen 127.0.0.1:8080
  llm-proxy-server serve --profiles examples/profiles.yaml --listen :8080
`),
		cmds.WithFlags(
			fields.New(
				"listen",
				fields.TypeString,
				fields.WithDefault("127.0.0.1:8080"),
				fields.WithHelp("Address to listen on"),
			),
			fields.New(
				"profiles",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Path to Geppetto profile YAML"),
			),
			fields.New(
				"byok-db",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Path to the BYOK SQLite database; enables bearer-token enforcement on /v1/*"),
			),
			fields.New(
				"byok-legacy-oidc-issuer",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Exact issuer assigned once when migrating existing pre-issuer users to schema v2"),
			),
			fields.New(
				"byok-master-key",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Vault master key (base64; env LLM_PROXY_BYOK_MASTER_KEY); use --byok-master-key-file in deployments"),
			),
			fields.New(
				"byok-master-key-file",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("File containing the vault master key; mutually exclusive with --byok-master-key"),
			),
			fields.New(
				"byok-session-secret",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Control-plane session-cookie secret (>=16 chars); use --byok-session-secret-file in deployments"),
			),
			fields.New(
				"byok-session-secret-file",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("File containing the session-cookie secret; mutually exclusive with --byok-session-secret"),
			),
			fields.New(
				"byok-session-absolute-lifetime",
				fields.TypeString,
				fields.WithDefault("24h"),
				fields.WithHelp("Absolute server-side browser session lifetime"),
			),
			fields.New(
				"byok-session-idle-timeout",
				fields.TypeString,
				fields.WithDefault("30m"),
				fields.WithHelp("Idle server-side browser session timeout"),
			),
			fields.New(
				"byok-agent-max-token-ttl",
				fields.TypeString,
				fields.WithDefault("8h"),
				fields.WithHelp("Maximum lifetime allowed for an agent-grant capability token"),
			),
			fields.New(
				"byok-meter-transient-failure-threshold",
				fields.TypeInteger,
				fields.WithDefault(3),
				fields.WithHelp("Consecutive transient metering write failures before inference fails closed"),
			),
			fields.New(
				"byok-meter-recovery-cooldown",
				fields.TypeString,
				fields.WithDefault("5s"),
				fields.WithHelp("Cooldown before a committed metering write probe may close the circuit"),
			),
			fields.New(
				"byok-oidc-issuer-url",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("OIDC issuer for control-plane login, e.g. http://127.0.0.1:18080/realms/byok"),
			),
			fields.New(
				"byok-oidc-client-id",
				fields.TypeString,
				fields.WithDefault("llm-proxy-web"),
				fields.WithHelp("OIDC client ID for the control plane"),
			),
			fields.New(
				"byok-oidc-client-secret",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("OIDC client secret; use --byok-oidc-client-secret-file in deployments"),
			),
			fields.New(
				"byok-oidc-client-secret-file",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("File containing the OIDC client secret; mutually exclusive with --byok-oidc-client-secret"),
			),
			fields.New(
				"byok-oidc-resource-client-id",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Confidential RFC 7662 resource client ID; enables /agent/v1/*"),
			),
			fields.New(
				"byok-oidc-resource-client-secret",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("RFC 7662 resource client secret; use the file option in deployments"),
			),
			fields.New(
				"byok-oidc-resource-client-secret-file",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("File containing the RFC 7662 resource client secret"),
			),
			fields.New(
				"byok-agent-audience",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Exact RFC 8707 audience accepted on /agent/v1/*"),
			),
			fields.New(
				"byok-agent-allowed-clients",
				fields.TypeString,
				fields.WithDefault("llm-proxy-agent"),
				fields.WithHelp("Comma-separated tiny-idp device client IDs accepted on /agent/v1/*"),
			),
			fields.New(
				"byok-introspection-positive-cache-ttl",
				fields.TypeString,
				fields.WithDefault("0s"),
				fields.WithHelp("Positive RFC 7662 cache TTL, maximum 5s"),
			),
			fields.New(
				"byok-introspection-negative-cache-ttl",
				fields.TypeString,
				fields.WithDefault("1s"),
				fields.WithHelp("Inactive-token RFC 7662 cache TTL, maximum 5s"),
			),
			fields.New(
				"byok-public-url",
				fields.TypeString,
				fields.WithDefault("http://127.0.0.1:8080"),
				fields.WithHelp("Externally visible base URL (OIDC redirect + origin checks)"),
			),
			fields.New(
				"byok-dev-user",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("DEV ONLY: enable passwordless /dev-login as this user"),
			),
		),
		cmds.WithSections(commandSettingsSection),
	)

	return &ServeCommand{CommandDescription: cmdDesc}, nil
}

func (c *ServeCommand) Run(ctx context.Context, parsedValues *values.Values) error {
	settings := &ServeSettings{}
	if err := parsedValues.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return err
	}
	return runServer(ctx, settings)
}

func runServer(ctx context.Context, opts *ServeSettings) error {
	if opts.ByokDB != "" && opts.Profiles == "" {
		return errors.New("--byok-db requires --profiles so BYOK requests use credential injection, model scoping, and usage metering")
	}

	masterKey, err := resolveSecretFile(opts.ByokMasterKey, opts.ByokMasterKeyFile, "--byok-master-key")
	if err != nil {
		return err
	}
	sessionSecret, err := resolveSecretFile(opts.ByokSessionSecret, opts.ByokSessionSecretFile, "--byok-session-secret")
	if err != nil {
		return err
	}
	oidcClientSecret, err := resolveSecretFile(opts.ByokOIDCClientSecret, opts.ByokOIDCClientSecretFile, "--byok-oidc-client-secret")
	if err != nil {
		return err
	}
	resourceClientSecret, err := resolveSecretFile(opts.ByokOIDCResourceClientSecret, opts.ByokOIDCResourceClientSecretFile, "--byok-oidc-resource-client-secret")
	if err != nil {
		return err
	}
	agentAPIConfigured := opts.ByokOIDCResourceClientID != "" || resourceClientSecret != "" || opts.ByokAgentAudience != ""
	if agentAPIConfigured && sessionSecret == "" {
		return errors.New("/agent/v1 requires the BYOK control plane and session secret")
	}

	var byokStore byokstorepkg.Store
	var byokVault *vault.Vault
	var meterHealth *byokmeter.Health
	var engineProvider runtimepkg.EngineProvider
	var usageRecorder runtimepkg.UsageRecorder
	if opts.ByokDB != "" {
		st, err := byoksqlite.Open(opts.ByokDB, byoksqlite.OpenOptions{LegacyOIDCIssuer: opts.ByokLegacyOIDCIssuer})
		if err != nil {
			return err
		}
		byokStore = st
		defer func() { _ = st.Close() }()
		if err := st.CheckMeteringHealth(ctx); err != nil {
			return errors.Join(errors.New("BYOK metering store failed its startup write probe"), err)
		}
		cooldownSetting := opts.ByokMeterRecoveryCooldown
		if cooldownSetting == "" {
			cooldownSetting = "5s"
		}
		cooldown, err := time.ParseDuration(cooldownSetting)
		if err != nil {
			return errors.New("--byok-meter-recovery-cooldown must be a valid duration")
		}
		meterHealth, err = byokmeter.NewHealth(st, st, byokmeter.HealthConfig{
			TransientFailureThreshold: opts.ByokMeterTransientFailureThreshold,
			RecoveryCooldown:          cooldown,
		})
		if err != nil {
			return err
		}
		v, err := byokcmds.OpenVault(masterKey)
		if err != nil {
			return err
		}
		byokVault = v
		engineProvider = &byokengines.VaultEngineProvider{Vault: v, Store: st}
		usageRecorder = &byokmeter.Recorder{Store: st, Health: meterHealth}
		log.Printf("BYOK enforcement enabled (db %s): per-user credentials, scoped models, metering", opts.ByokDB)
	} else {
		log.Printf("WARNING: BYOK disabled — /v1/* is unauthenticated (pass --byok-db to enable)")
	}

	var modelLister server.ModelLister
	var completionService server.CompletionService
	var chatCompletionService server.ChatCompletionService
	var profileResolver profilespkg.ProfileResolver
	if opts.Profiles != "" {
		resolver, err := profilespkg.NewYAMLResolver(opts.Profiles)
		if err != nil {
			return err
		}
		profileResolver = resolver
		modelLister = profileModelLister{resolver: resolver}
		completionService = &runtimepkg.GeppettoCompletionService{Profiles: resolver, Engines: engineProvider, Usage: usageRecorder}
		chatCompletionService = &runtimepkg.GeppettoChatCompletionService{Profiles: resolver, Engines: engineProvider, Usage: usageRecorder}
	}
	if byokStore != nil {
		modelLister = &authmw.ScopedModelLister{Inner: modelLister}
	}

	srv := server.New(server.Options{
		CompletionService: completionService, ChatCompletionService: chatCompletionService,
		ModelLister: modelLister, Readiness: meterHealth,
	})
	handler := http.Handler(srv.Handler())
	if byokStore != nil {
		handler = authmw.TokenAuthWithMeterHealth(byokStore, meterHealth, handler)
	}

	// Control plane: mounted when BYOK is on and a session secret is set.
	if byokStore != nil && sessionSecret != "" {
		sessionMaxAge, err := time.ParseDuration(opts.ByokSessionAbsoluteLifetime)
		if err != nil || sessionMaxAge <= 0 {
			return errors.New("--byok-session-absolute-lifetime must be a positive duration")
		}
		sessionIdleTimeout, err := time.ParseDuration(opts.ByokSessionIdleTimeout)
		if err != nil || sessionIdleTimeout <= 0 {
			return errors.New("--byok-session-idle-timeout must be a positive duration")
		}
		agentMaxTokenTTL, err := time.ParseDuration(opts.ByokAgentMaxTokenTTL)
		if err != nil || agentMaxTokenTTL <= 0 {
			return errors.New("--byok-agent-max-token-ttl must be a positive duration")
		}
		if profileResolver == nil {
			return errors.New("BYOK agent grants require --profiles")
		}
		profileDescriptors, err := profileResolver.ListProfiles(ctx)
		if err != nil {
			return fmt.Errorf("list profiles for BYOK agent grants: %w", err)
		}
		allowedGrantModels := make([]string, 0, len(profileDescriptors))
		for _, descriptor := range profileDescriptors {
			allowedGrantModels = append(allowedGrantModels, descriptor.ID)
		}
		var oidcCfg *byokweb.OIDCConfig
		if opts.ByokOIDCIssuerURL != "" {
			oidcCfg = &byokweb.OIDCConfig{
				IssuerURL:    opts.ByokOIDCIssuerURL,
				ClientID:     opts.ByokOIDCClientID,
				ClientSecret: oidcClientSecret,
				PublicURL:    opts.ByokPublicURL,
			}
		}
		webServer, err := byokweb.NewServer(ctx, byokweb.Config{
			Store: byokStore, Vault: byokVault,
			SessionSecret: sessionSecret, SessionMaxAge: sessionMaxAge,
			SessionIdleTimeout: sessionIdleTimeout,
			AgentMaxTokenTTL:   agentMaxTokenTTL,
			AllowedGrantModels: allowedGrantModels,
			OIDC:               oidcCfg,
			DevUser:            opts.ByokDevUser,
		})
		if err != nil {
			return err
		}
		outer := http.NewServeMux()
		webServer.Register(outer)
		if agentAPIConfigured {
			if opts.ByokOIDCIssuerURL == "" || opts.ByokOIDCResourceClientID == "" || resourceClientSecret == "" || opts.ByokAgentAudience == "" {
				return errors.New("/agent/v1 requires OIDC issuer, resource client ID, resource client secret, and exact agent audience")
			}
			positiveTTL, err := time.ParseDuration(opts.ByokIntrospectionPositiveCacheTTL)
			if err != nil {
				return errors.New("--byok-introspection-positive-cache-ttl must be a duration")
			}
			negativeTTL, err := time.ParseDuration(opts.ByokIntrospectionNegativeCacheTTL)
			if err != nil {
				return errors.New("--byok-introspection-negative-cache-ttl must be a duration")
			}
			var allowedClients []string
			for _, clientID := range strings.Split(opts.ByokAgentAllowedClients, ",") {
				if clientID = strings.TrimSpace(clientID); clientID != "" {
					allowedClients = append(allowedClients, clientID)
				}
			}
			authenticator, err := byokoidcauth.New(ctx, byokoidcauth.Config{
				IssuerURL: opts.ByokOIDCIssuerURL, ResourceClientID: opts.ByokOIDCResourceClientID,
				ClientSecret: []byte(resourceClientSecret), Audience: opts.ByokAgentAudience,
				AllowedClients: allowedClients, PositiveCacheTTL: positiveTTL, NegativeCacheTTL: negativeTTL,
			})
			if err != nil {
				return err
			}
			agentServer, err := byokagentapi.New(byokStore, authenticator)
			if err != nil {
				return err
			}
			agentServer.Register(outer)
		}
		outer.Handle("/", handler)
		handler = outer
		log.Printf("BYOK control plane enabled at /app (OIDC: %v, dev login: %v)", oidcCfg != nil, opts.ByokDevUser != "")
	}
	httpServer := &http.Server{
		Addr:              opts.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("llm-proxy-server listening on %s", opts.Listen)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// resolveSecretFile reads an optional deployment secret without ever putting
// its contents in a process argument. Inline flags remain for local use, but a
// caller must choose exactly one source for any particular secret.
func resolveSecretFile(inline, path, flag string) (string, error) {
	if inline != "" && path != "" {
		return "", errors.New(flag + " and " + flag + "-file are mutually exclusive")
	}
	if path == "" {
		return inline, nil
	}
	contents, err := readDeploymentSecretFile(path)
	if err != nil {
		return "", errors.New("read " + flag + " file: " + err.Error())
	}
	value := strings.TrimSpace(string(contents))
	if value == "" {
		return "", errors.New(flag + " file must not be empty")
	}
	return value, nil
}

type profileModelLister struct {
	resolver profilespkg.ProfileResolver
}

func (l profileModelLister) ListModels(ctx context.Context) ([]server.ModelDescriptor, error) {
	profiles, err := l.resolver.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]server.ModelDescriptor, 0, len(profiles))
	for _, p := range profiles {
		models = append(models, server.ModelDescriptor{ID: p.ID, Object: "model", OwnedBy: "geppetto-profile"})
	}
	return models, nil
}
