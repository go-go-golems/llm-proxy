// Package byok defines the Glazed command group for BYOK administration:
// users, vault credentials, and minted tokens. All flags are Glazed fields,
// so every one of them can also be set via LLM_PROXY_<FIELD> environment
// variables (e.g. --byok-master-key ⇔ LLM_PROXY_BYOK_MASTER_KEY).
package byok

import (
	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/sqlite"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

// AppName drives the env prefix for all llm-proxy commands (LLM_PROXY_*).
// Underscored on purpose: glazed uppercases the app name verbatim as the env
// prefix, so "llm-proxy" would demand unusable LLM-PROXY_* variable names.
const AppName = "llm_proxy"

func dbField() *fields.Definition {
	return fields.New(
		"byok-db",
		fields.TypeString,
		fields.WithDefault("var/byok.db"),
		fields.WithHelp("Path to the BYOK SQLite database (env LLM_PROXY_BYOK_DB)"),
	)
}

func masterKeyField() *fields.Definition {
	return fields.New(
		"byok-master-key",
		fields.TypeString,
		fields.WithDefault(""),
		fields.WithHelp("Vault master key, base64 (env LLM_PROXY_BYOK_MASTER_KEY; generate with 'byok keygen')"),
	)
}

func userField() *fields.Definition {
	return fields.New(
		"user",
		fields.TypeString,
		fields.WithDefault(""),
		fields.WithHelp("Owner username"),
	)
}

// NOTE: settings structs repeat the DB field instead of embedding a shared
// struct — glazed's DecodeInto only walks top-level tagged fields and
// silently skips embedded structs.

func openStore(dbPath string) (store.Store, error) {
	return sqlite.Open(dbPath)
}

// OpenVault builds the credential vault from a base64 master key. Exported
// because the serve command shares the same key-resolution semantics.
func OpenVault(masterKey string) (*vault.Vault, error) {
	if masterKey == "" {
		return nil, errors.New("no master key: pass --byok-master-key or set LLM_PROXY_BYOK_MASTER_KEY (generate one with 'byok keygen')")
	}
	return vault.NewFromBase64(masterKey)
}

// build wraps a Glazed command into cobra with the standard llm-proxy parser
// config (short help on the default section, env loading via AppName).
func build(cmd cmds.Command) (*cobra.Command, error) {
	return cli.BuildCobraCommandFromCommand(cmd,
		cli.WithParserConfig(cli.CobraParserConfig{
			ShortHelpSections: []string{schema.DefaultSlug},
			AppName:           AppName,
		}),
	)
}
