package byok

import (
	"context"
	"fmt"
	"io"

	"github.com/go-go-golems/glazed/pkg/cli"
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/go-go-golems/glazed/pkg/cmds/values"

	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

type KeygenCommand struct {
	*cmds.CommandDescription
}

var _ cmds.WriterCommand = &KeygenCommand{}

func NewKeygenCommand() (*KeygenCommand, error) {
	commandSettingsSection, err := cli.NewCommandSettingsSection()
	if err != nil {
		return nil, err
	}
	return &KeygenCommand{CommandDescription: cmds.NewCommandDescription(
		"keygen",
		cmds.WithShort("Generate a vault master key (base64, printed to stdout)"),
		cmds.WithLong(`Generate a vault master key.

Store it securely and pass it via --byok-master-key or LLM_PROXY_BYOK_MASTER_KEY.

Example:

  export LLM_PROXY_BYOK_MASTER_KEY=$(llm-proxy-server byok keygen)
`),
		cmds.WithSections(commandSettingsSection),
	)}, nil
}

func (c *KeygenCommand) RunIntoWriter(_ context.Context, _ *values.Values, w io.Writer) error {
	key, err := vault.GenerateKeyBase64()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, key)
	return err
}
