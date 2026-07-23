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
)

// --- user add ---

type UserAddCommand struct {
	*cmds.CommandDescription
}

type userAddSettings struct {
	DB       string `glazed:"byok-db"`
	Username string `glazed:"username"`
	Email    string `glazed:"email"`
	Issuer   string `glazed:"issuer"`
	Subject  string `glazed:"subject"`
}

var _ cmds.WriterCommand = &UserAddCommand{}

func NewUserAddCommand() (*UserAddCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &UserAddCommand{CommandDescription: cmds.NewCommandDescription(
		"add",
		cmds.WithShort("Create (or update) a BYOK user"),
		cmds.WithLong(`Create (or update) a BYOK user.

Examples:

  llm-proxy-server byok user add --username alice --email alice@example.com
`),
		cmds.WithFlags(
			dbField(),
			fields.New("username", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Username (required)")),
			fields.New("email", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Email address")),
			fields.New("issuer", fields.TypeString, fields.WithDefault("urn:llm-proxy:operator"), fields.WithHelp("Identity issuer")),
			fields.New("subject", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Subject (default local:<username>)")),
		),
		cmds.WithSections(commandSettingsSection),
	)}, nil
}

func (c *UserAddCommand) RunIntoWriter(ctx context.Context, vals *values.Values, w io.Writer) error {
	s := &userAddSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	if s.Username == "" {
		return errors.New("--username is required")
	}
	if s.Subject == "" {
		s.Subject = "local:" + s.Username
	}
	st, err := openStore(s.DB)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	u, err := st.UpsertUser(ctx, store.User{OIDCIssuer: s.Issuer, OIDCSubject: s.Subject, Username: s.Username, Email: s.Email})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "user %s (id %s, issuer %s, subject %s)\n", u.Username, u.ID, u.OIDCIssuer, u.OIDCSubject)
	return err
}

// --- user list ---

type UserListCommand struct {
	*cmds.CommandDescription
}

type userListSettings struct {
	DB string `glazed:"byok-db"`
}

var _ cmds.GlazeCommand = &UserListCommand{}

func NewUserListCommand() (*UserListCommand, error) {
	glazedSection, err := settings.NewGlazedSchema()
	if err != nil {
		return nil, err
	}
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &UserListCommand{CommandDescription: cmds.NewCommandDescription(
		"list",
		cmds.WithShort("List BYOK users"),
		cmds.WithFlags(dbField()),
		cmds.WithSections(glazedSection, commandSettingsSection),
	)}, nil
}

func (c *UserListCommand) RunIntoGlazeProcessor(ctx context.Context, vals *values.Values, gp middlewares.Processor) error {
	s := &userListSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, s); err != nil {
		return err
	}
	st, err := openStore(s.DB)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	users, err := st.ListUsers(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		row := types.NewRow(
			types.MRP("id", u.ID),
			types.MRP("username", u.Username),
			types.MRP("email", u.Email),
			types.MRP("issuer", u.OIDCIssuer),
			types.MRP("subject", u.OIDCSubject),
			types.MRP("created_at", u.CreatedAt),
		)
		if err := gp.AddRow(ctx, row); err != nil {
			return err
		}
	}
	return nil
}
