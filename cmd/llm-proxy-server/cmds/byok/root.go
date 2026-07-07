package byok

import (
	"github.com/go-go-golems/glazed/pkg/cmds"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// NewCommand builds the `byok` command group with its user/token/credential
// subgroups and keygen. All subcommands are Glazed commands.
func NewCommand() (*cobra.Command, error) {
	byokCmd := &cobra.Command{
		Use:   "byok",
		Short: "Manage BYOK users, credentials, and minted tokens",
	}

	groups := []struct {
		use      string
		short    string
		children []func() (cmds.Command, error)
	}{
		{
			use: "user", short: "Manage BYOK users",
			children: []func() (cmds.Command, error){
				func() (cmds.Command, error) { return NewUserAddCommand() },
				func() (cmds.Command, error) { return NewUserListCommand() },
			},
		},
		{
			use: "token", short: "Mint, list, and revoke BYOK tokens",
			children: []func() (cmds.Command, error){
				func() (cmds.Command, error) { return NewTokenMintCommand() },
				func() (cmds.Command, error) { return NewTokenListCommand() },
				func() (cmds.Command, error) { return NewTokenRevokeCommand() },
			},
		},
		{
			use: "credential", short: "Manage vault credentials (provider API keys)",
			children: []func() (cmds.Command, error){
				func() (cmds.Command, error) { return NewCredentialAddCommand() },
				func() (cmds.Command, error) { return NewCredentialListCommand() },
				func() (cmds.Command, error) { return NewCredentialRmCommand() },
			},
		},
	}
	for _, g := range groups {
		groupCmd := &cobra.Command{Use: g.use, Short: g.short}
		for _, newChild := range g.children {
			child, err := newChild()
			if err != nil {
				return nil, errors.Wrapf(err, "build byok %s command", g.use)
			}
			cobraChild, err := build(child)
			if err != nil {
				return nil, errors.Wrapf(err, "wrap byok %s command", g.use)
			}
			groupCmd.AddCommand(cobraChild)
		}
		byokCmd.AddCommand(groupCmd)
	}

	keygen, err := NewKeygenCommand()
	if err != nil {
		return nil, err
	}
	cobraKeygen, err := build(keygen)
	if err != nil {
		return nil, err
	}
	byokCmd.AddCommand(cobraKeygen)

	return byokCmd, nil
}
