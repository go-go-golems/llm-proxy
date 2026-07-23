package byok

import (
	"context"
	"fmt"
	"io"
	"time"

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
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
)

// --- token mint ---

type TokenMintCommand struct {
	*cmds.CommandDescription
}

type tokenMintSettings struct {
	DB          string   `glazed:"byok-db"`
	User        string   `glazed:"user"`
	Name        string   `glazed:"name"`
	Models      []string `glazed:"models"`
	Credentials []string `glazed:"credentials"`
	MaxTokens   int      `glazed:"max-tokens"`
	MaxRequests int      `glazed:"max-requests"`
	RPM         int      `glazed:"rpm"`
	ExpiresDays int      `glazed:"expires-days"`
}

var _ cmds.WriterCommand = &TokenMintCommand{}

func NewTokenMintCommand() (*TokenMintCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &TokenMintCommand{CommandDescription: cmds.NewCommandDescription(
		"mint",
		cmds.WithShort("Mint a scoped token (the plaintext is printed exactly once)"),
		cmds.WithLong(`Mint a scoped BYOK token.

Examples:

  llm-proxy-server byok token mint --user alice --name ci \
    --models sonnet,gpt-* --credentials <cred-id> \
    --max-tokens 2000000 --rpm 60 --expires-days 30
`),
		cmds.WithFlags(
			dbField(),
			userField(),
			fields.New("name", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Token label (required)")),
			fields.New("models", fields.TypeStringList, fields.WithDefault([]string{}), fields.WithHelp("Allowed model slugs or globs (required)")),
			fields.New("credentials", fields.TypeStringList, fields.WithDefault([]string{}), fields.WithHelp("Credential IDs the token may use")),
			fields.New("max-tokens", fields.TypeInteger, fields.WithDefault(0), fields.WithHelp("Total token budget, prompt+completion (0 = unlimited)")),
			fields.New("max-requests", fields.TypeInteger, fields.WithDefault(0), fields.WithHelp("Request budget (0 = unlimited)")),
			fields.New("rpm", fields.TypeInteger, fields.WithDefault(0), fields.WithHelp("Rate limit in requests/minute (0 = unlimited)")),
			fields.New("expires-days", fields.TypeInteger, fields.WithDefault(0), fields.WithHelp("Expiry in days from now (0 = no expiry)")),
		),
		cmds.WithSections(commandSettingsSection),
	)}, nil
}

func (c *TokenMintCommand) RunIntoWriter(ctx context.Context, vals *values.Values, w io.Writer) error {
	s := &tokenMintSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if s.User == "" || s.Name == "" || len(s.Models) == 0 {
		return errors.New("--user, --name, and --models are required")
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
	for _, id := range s.Credentials {
		if _, err := st.GetCredential(ctx, u.ID, id); err != nil {
			return errors.Wrapf(err, "credential %q", id)
		}
	}
	raw, hash, err := tokens.Mint()
	if err != nil {
		return err
	}
	tok := store.Token{
		UserID: u.ID, TokenHash: hash, Name: s.Name,
		CredentialIDs: s.Credentials, AllowedModels: s.Models,
	}
	setBudget := func(v int, dst **int64) {
		if v > 0 {
			n := int64(v)
			*dst = &n
		}
	}
	setBudget(s.MaxTokens, &tok.MaxTotalTokens)
	setBudget(s.MaxRequests, &tok.MaxRequests)
	setBudget(s.RPM, &tok.RateLimitRPM)
	if s.ExpiresDays > 0 {
		exp := time.Now().UTC().Add(time.Duration(s.ExpiresDays) * 24 * time.Hour)
		tok.ExpiresAt = &exp
	}
	minted, err := st.MintTokenAudited(ctx, tok)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "token %s minted for %s (id %s)\nTHIS IS THE ONLY TIME THE TOKEN IS SHOWN:\n%s\n", s.Name, u.Username, minted.ID, raw)
	return err
}

// --- token list ---

type TokenListCommand struct {
	*cmds.CommandDescription
}

type tokenListSettings struct {
	DB   string `glazed:"byok-db"`
	User string `glazed:"user"`
}

var _ cmds.GlazeCommand = &TokenListCommand{}

func NewTokenListCommand() (*TokenListCommand, error) {
	glazedSection, err := settings.NewGlazedSchema()
	if err != nil {
		return nil, err
	}
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &TokenListCommand{CommandDescription: cmds.NewCommandDescription(
		"list",
		cmds.WithShort("List a user's tokens with usage"),
		cmds.WithFlags(dbField(), userField()),
		cmds.WithSections(glazedSection, commandSettingsSection),
	)}, nil
}

func (c *TokenListCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &tokenListSettings{}
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
	toks, err := st.ListTokensByUser(ctx, u.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, t := range toks {
		counters, err := st.GetCounters(ctx, t.ID)
		if err != nil {
			return err
		}
		status := "active"
		switch {
		case t.RevokedAt != nil:
			status = "revoked"
		case t.ExpiresAt != nil && now.After(*t.ExpiresAt):
			status = "expired"
		}
		row := types.NewRow(
			types.MRP("id", t.ID),
			types.MRP("name", t.Name),
			types.MRP("models", t.AllowedModels),
			types.MRP("used_tokens", counters.TotalTokens),
			types.MRP("used_requests", counters.TotalRequests),
			types.MRP("max_tokens", derefInt(t.MaxTotalTokens)),
			types.MRP("max_requests", derefInt(t.MaxRequests)),
			types.MRP("rpm", derefInt(t.RateLimitRPM)),
			types.MRP("status", status),
			types.MRP("last_used_at", t.LastUsedAt),
		)
		if err := gp.AddRow(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// --- token revoke ---

type TokenRevokeCommand struct {
	*cmds.CommandDescription
}

type tokenRevokeSettings struct {
	DB   string `glazed:"byok-db"`
	User string `glazed:"user"`
	ID   string `glazed:"id"`
}

var _ cmds.WriterCommand = &TokenRevokeCommand{}

func NewTokenRevokeCommand() (*TokenRevokeCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &TokenRevokeCommand{CommandDescription: cmds.NewCommandDescription(
		"revoke",
		cmds.WithShort("Revoke a token (takes effect on the next request)"),
		cmds.WithFlags(
			dbField(),
			userField(),
			fields.New("id", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Token ID (required)")),
		),
		cmds.WithSections(commandSettingsSection),
	)}, nil
}

func (c *TokenRevokeCommand) RunIntoWriter(ctx context.Context, vals *values.Values, w io.Writer) error {
	s := &tokenRevokeSettings{}
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
	if err := st.RevokeTokenAudited(ctx, u.ID, s.ID); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "token %s revoked\n", s.ID)
	return err
}
