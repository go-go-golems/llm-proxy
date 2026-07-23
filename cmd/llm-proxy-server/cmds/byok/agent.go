package byok

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/fields"
	"github.com/go-go-golems/glazed/pkg/cmds/schema"
	"github.com/go-go-golems/glazed/pkg/cmds/values"
	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/deviceclient"
)

type agentSettings struct {
	Issuer   string `glazed:"issuer"`
	ClientID string `glazed:"client-id"`
	Audience string `glazed:"audience"`
	Broker   string `glazed:"broker"`
	GrantID  string `glazed:"grant-id"`
	Cache    string `glazed:"cache"`
}

func agentFields(includeNetwork, includeGrant bool) []*fields.Definition {
	definitions := []*fields.Definition{fields.New("cache", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Credential cache path (default: user config directory)"))}
	if includeNetwork {
		definitions = append(definitions,
			fields.New("issuer", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Exact tiny-idp issuer URL")),
			fields.New("client-id", fields.TypeString, fields.WithDefault("llm-proxy-agent"), fields.WithHelp("Public RFC 8628 client ID")),
			fields.New("audience", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Exact llm-proxy /agent/v1 audience")),
			fields.New("broker", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Public llm-proxy base URL")),
		)
	}
	if includeGrant {
		definitions = append(definitions, fields.New("grant-id", fields.TypeString, fields.WithDefault(""), fields.WithHelp("Pre-approved grant ID; required when multiple grants are eligible")))
	}
	return definitions
}

func decodeAgentSettings(vals *values.Values) (*agentSettings, error) {
	settings := &agentSettings{}
	if err := vals.DecodeSectionInto(schema.DefaultSlug, settings); err != nil {
		return nil, err
	}
	if settings.Cache == "" {
		path, err := deviceclient.DefaultCachePath()
		if err != nil {
			return nil, err
		}
		settings.Cache = path
	}
	return settings, nil
}

type AgentLoginCommand struct{ *cmds.CommandDescription }

var _ cmds.WriterCommand = &AgentLoginCommand{}

func NewAgentLoginCommand() (*AgentLoginCommand, error) {
	section, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &AgentLoginCommand{CommandDescription: cmds.NewCommandDescription("login", cmds.WithShort("Authenticate through RFC 8628 and cache one scoped llmp capability"), cmds.WithFlags(agentFields(true, true)...), cmds.WithSections(section))}, nil
}

func (c *AgentLoginCommand) RunIntoWriter(ctx context.Context, vals *values.Values, w io.Writer) error {
	settings, err := decodeAgentSettings(vals)
	if err != nil {
		return err
	}
	if settings.Issuer == "" || settings.Audience == "" || settings.Broker == "" {
		return errors.New("--issuer, --audience, and --broker are required")
	}
	instanceID, err := deviceclient.LoadOrCreateClientInstanceID(settings.Cache)
	if err != nil {
		return err
	}
	client, err := deviceclient.New(ctx, deviceclient.Config{IssuerURL: settings.Issuer, ClientID: settings.ClientID, Audience: settings.Audience, BrokerURL: settings.Broker})
	if err != nil {
		return err
	}
	credential, err := client.Login(ctx, instanceID, settings.GrantID, func(prompt deviceclient.Prompt) error {
		verification := prompt.VerificationURIComplete
		if verification == "" {
			verification = prompt.VerificationURI
		}
		_, err := fmt.Fprintf(os.Stderr, "Open %s and authorize this device. User code: %s\n", verification, prompt.UserCode)
		return err
	})
	if err != nil {
		return err
	}
	if err := deviceclient.SaveCredential(settings.Cache, credential); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "authenticated broker=%s grant=%s expires=%s models=%v cache=%s\n", credential.BrokerURL, credential.GrantID, credential.ExpiresAt.Format(time.RFC3339), credential.AllowedModels, settings.Cache)
	return err
}

func (c *AgentLoginCommand) Description() *cmds.CommandDescription { return c.CommandDescription }

type AgentStatusCommand struct{ *cmds.CommandDescription }

var _ cmds.WriterCommand = &AgentStatusCommand{}

func NewAgentStatusCommand() (*AgentStatusCommand, error) {
	section, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &AgentStatusCommand{CommandDescription: cmds.NewCommandDescription("status", cmds.WithShort("Show cached agent capability metadata without revealing the token"), cmds.WithFlags(agentFields(false, false)...), cmds.WithSections(section))}, nil
}

func (c *AgentStatusCommand) RunIntoWriter(_ context.Context, vals *values.Values, w io.Writer) error {
	settings, err := decodeAgentSettings(vals)
	if err != nil {
		return err
	}
	credential, err := deviceclient.LoadCredential(settings.Cache)
	if err != nil {
		return err
	}
	status := "active"
	if !credential.ExpiresAt.After(time.Now().UTC()) {
		status = "expired"
	}
	_, err = fmt.Fprintf(w, "%s broker=%s grant=%s expires=%s models=%v cache=%s\n", status, credential.BrokerURL, credential.GrantID, credential.ExpiresAt.Format(time.RFC3339), credential.AllowedModels, settings.Cache)
	return err
}
func (c *AgentStatusCommand) Description() *cmds.CommandDescription { return c.CommandDescription }

type AgentLogoutCommand struct{ *cmds.CommandDescription }

var _ cmds.WriterCommand = &AgentLogoutCommand{}

func NewAgentLogoutCommand() (*AgentLogoutCommand, error) {
	section, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &AgentLogoutCommand{CommandDescription: cmds.NewCommandDescription("logout", cmds.WithShort("Delete the locally cached agent capability"), cmds.WithFlags(agentFields(false, false)...), cmds.WithSections(section))}, nil
}
func (c *AgentLogoutCommand) RunIntoWriter(_ context.Context, vals *values.Values, w io.Writer) error {
	settings, err := decodeAgentSettings(vals)
	if err != nil {
		return err
	}
	if err := deviceclient.DeleteCredential(settings.Cache); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "logged_out cache=%s\n", settings.Cache)
	return err
}
func (c *AgentLogoutCommand) Description() *cmds.CommandDescription { return c.CommandDescription }
