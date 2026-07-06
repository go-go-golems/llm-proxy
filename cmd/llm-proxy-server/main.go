package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/logging"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/help"
	help_cmd "github.com/go-go-golems/glazed/pkg/help/cmd"
	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	byokengines "github.com/go-go-golems/llm-proxy/pkg/byok/engines"
	byokmeter "github.com/go-go-golems/llm-proxy/pkg/byok/meter"
	byokstorepkg "github.com/go-go-golems/llm-proxy/pkg/byok/store"
	byoksqlite "github.com/go-go-golems/llm-proxy/pkg/byok/store/sqlite"
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
	Listen        string `glazed:"listen"`
	Profiles      string `glazed:"profiles"`
	ByokDB        string `glazed:"byok-db"`
	ByokMasterKey string `glazed:"byok-master-key"`
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
		}),
	)
	cobra.CheckErr(err)
	rootCmd.AddCommand(serveCobraCmd)
	rootCmd.AddCommand(newByokCommand())

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

The server exposes /healthz, /v1/models, /v1/completions, and /v1/chat/completions. Provider setup lives in Geppetto profile YAML; without a profiles file the server still exposes health checks and static stub responses.

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
				"byok-master-key",
				fields.TypeString,
				fields.WithDefault(""),
				fields.WithHelp("Vault master key (base64); default $"+masterKeyEnv),
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
	var byokStore byokstorepkg.Store
	var engineProvider runtimepkg.EngineProvider
	var usageRecorder runtimepkg.UsageRecorder
	if opts.ByokDB != "" {
		st, err := byoksqlite.Open(opts.ByokDB)
		if err != nil {
			return err
		}
		byokStore = st
		defer func() { _ = st.Close() }()
		v, err := openVault(opts.ByokMasterKey)
		if err != nil {
			return err
		}
		engineProvider = &byokengines.VaultEngineProvider{Vault: v, Store: st}
		usageRecorder = &byokmeter.Recorder{Store: st}
		log.Printf("BYOK enforcement enabled (db %s): per-user credentials, scoped models, metering", opts.ByokDB)
	} else {
		log.Printf("WARNING: BYOK disabled — /v1/* is unauthenticated (pass --byok-db to enable)")
	}

	var modelLister server.ModelLister
	var completionService server.CompletionService
	var chatCompletionService server.ChatCompletionService
	if opts.Profiles != "" {
		resolver, err := profilespkg.NewYAMLResolver(opts.Profiles)
		if err != nil {
			return err
		}
		modelLister = profileModelLister{resolver: resolver}
		completionService = &runtimepkg.GeppettoCompletionService{Profiles: resolver, Engines: engineProvider, Usage: usageRecorder}
		chatCompletionService = &runtimepkg.GeppettoChatCompletionService{Profiles: resolver, Engines: engineProvider, Usage: usageRecorder}
	}
	if byokStore != nil {
		modelLister = &authmw.ScopedModelLister{Inner: modelLister}
	}

	srv := server.New(server.Options{CompletionService: completionService, ChatCompletionService: chatCompletionService, ModelLister: modelLister})
	handler := http.Handler(srv.Handler())
	if byokStore != nil {
		handler = authmw.TokenAuth(byokStore, handler)
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
