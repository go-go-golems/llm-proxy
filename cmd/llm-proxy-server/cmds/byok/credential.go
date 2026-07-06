package byok

import (
	"context"
	"fmt"
	"io"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/go-go-golems/glazed/pkg/middlewares"
	"github.com/go-go-golems/glazed/pkg/settings"
	"github.com/go-go-golems/glazed/pkg/types"
	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

// --- credential add ---

type CredentialAddCommand struct {
	*cmds.CommandDescription
}

type credentialAddSettings struct {
	DB        string `glazed:"byok-db"`
	MasterKey string `glazed:"byok-master-key"`
	User      string `glazed:"user"`
	Provider  string `glazed:"provider"`
	APIType   string `glazed:"api-type"`
	Label     string `glazed:"label"`
	Secret    string `glazed:"byok-secret"`
}

var _ cmds.WriterCommand = &CredentialAddCommand{}

func NewCredentialAddCommand() (*CredentialAddCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &CredentialAddCommand{CommandDescription: cmds.NewCommandDescription(
		"add",
		cmds.WithShort("Store a provider API key, encrypted at rest"),
		cmds.WithLong(`Store a provider API key in the vault, encrypted at rest.

Prefer passing the secret via the environment so it stays out of shell history:

  LLM_PROXY_BYOK_SECRET=$ANTHROPIC_API_KEY \
  LLM_PROXY_BYOK_MASTER_KEY=<key> \
  llm-proxy-server byok credential add --user alice --provider anthropic --api-type claude
`),
		cmds.WithFlags(
			dbField(),
			masterKeyField(),
			userField(),
			fields.New("provider", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Provider name: anthropic, openai, ... (required)")),
			fields.New("api-type", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Geppetto api-type: claude, openai, ... (required)")),
			fields.New("label", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Display label (default: provider name)")),
			fields.New("byok-secret", fields.TypeString, fields.WithDefault(""), fields.WithHelp("The provider API key (prefer env LLM_PROXY_BYOK_SECRET)")),
		),
		cmds.WithSections(commandSettingsSection),
	)}, nil
}

func (c *CredentialAddCommand) RunIntoWriter(ctx context.Context, vals *values.Values, w io.Writer) error {
	s := &credentialAddSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if s.User == "" || s.Provider == "" || s.APIType == "" {
		return errors.New("--user, --provider, and --api-type are required")
	}
	if s.Secret == "" {
		return errors.New("no secret: set LLM_PROXY_BYOK_SECRET (preferred) or pass --byok-secret")
	}
	v, err := OpenVault(s.MasterKey)
	if err != nil {
		return err
	}
	st, err := openStore(s.DB)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	u, err := st.GetUserByUsername(ctx, s.User)
	if err != nil {
		return errors.Wrapf(err, "user %q", s.User)
	}
	credID := store.NewID()
	cipherBlob, err := v.Encrypt(credID, []byte(s.Secret))
	if err != nil {
		return err
	}
	label := s.Label
	if label == "" {
		label = s.Provider
	}
	cred, err := st.CreateCredential(ctx, store.Credential{
		ID: credID, UserID: u.ID, Provider: s.Provider, APIType: s.APIType,
		Label: label, SecretCipher: cipherBlob, SecretLast4: vault.Last4(s.Secret),
	})
	if err != nil {
		return err
	}
	_ = st.AppendEvent(ctx, store.AuditEvent{
		UserID: u.ID, EventType: "credential.created",
		Payload: []byte(fmt.Sprintf(`{"credential_id":%q,"provider":%q}`, cred.ID, s.Provider)),
	})
	_, err = fmt.Fprintf(w, "credential %s stored for %s (%s, %s)\n", cred.ID, u.Username, s.Provider, cred.SecretLast4)
	return err
}

// --- credential list ---

type CredentialListCommand struct {
	*cmds.CommandDescription
}

type credentialListSettings struct {
	DB   string `glazed:"byok-db"`
	User string `glazed:"user"`
}

var _ cmds.GlazeCommand = &CredentialListCommand{}

func NewCredentialListCommand() (*CredentialListCommand, error) {
	glazedSection, err := settings.NewGlazedSchema()
	if err != nil {
		return nil, err
	}
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &CredentialListCommand{CommandDescription: cmds.NewCommandDescription(
		"list",
		cmds.WithShort("List a user's credentials (never shows secrets)"),
		cmds.WithFlags(dbField(), userField()),
		cmds.WithSections(glazedSection, commandSettingsSection),
	)}, nil
}

func (c *CredentialListCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &credentialListSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if s.User == "" {
		return errors.New("--user is required")
	}
	st, err := openStore(s.DB)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	u, err := st.GetUserByUsername(ctx, s.User)
	if err != nil {
		return errors.Wrapf(err, "user %q", s.User)
	}
	creds, err := st.ListCredentialsByUser(ctx, u.ID)
	if err != nil {
		return err
	}
	for _, cred := range creds {
		row := types.NewRow(
			types.MRP("id", cred.ID),
			types.MRP("provider", cred.Provider),
			types.MRP("api_type", cred.APIType),
			types.MRP("label", cred.Label),
			types.MRP("secret", cred.SecretLast4),
			types.MRP("disabled", cred.Disabled),
			types.MRP("created_at", cred.CreatedAt),
		)
		if err := gp.AddRow(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// --- credential rm ---

type CredentialRmCommand struct {
	*cmds.CommandDescription
}

type credentialRmSettings struct {
	DB   string `glazed:"byok-db"`
	User string `glazed:"user"`
	ID   string `glazed:"id"`
}

var _ cmds.WriterCommand = &CredentialRmCommand{}

func NewCredentialRmCommand() (*CredentialRmCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &CredentialRmCommand{CommandDescription: cmds.NewCommandDescription(
		"rm",
		cmds.WithShort("Delete a credential (revokes tokens bound only to it)"),
		cmds.WithFlags(
			dbField(),
			userField(),
			fields.New("id", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Credential ID (required)")),
		),
		cmds.WithSections(commandSettingsSection),
	)}, nil
}

func (c *CredentialRmCommand) RunIntoWriter(ctx context.Context, vals *values.Values, w io.Writer) error {
	s := &credentialRmSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if s.User == "" || s.ID == "" {
		return errors.New("--user and --id are required")
	}
	st, err := openStore(s.DB)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	u, err := st.GetUserByUsername(ctx, s.User)
	if err != nil {
		return errors.Wrapf(err, "user %q", s.User)
	}
	if err := st.DeleteCredential(ctx, u.ID, s.ID); err != nil {
		return err
	}
	_ = st.AppendEvent(ctx, store.AuditEvent{
		UserID: u.ID, EventType: "credential.deleted",
		Payload: []byte(fmt.Sprintf(`{"credential_id":%q}`, s.ID)),
	})
	_, err = fmt.Fprintf(w, "credential %s deleted\n", s.ID)
	return err
}
