package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/sqlite"
	"github.com/go-go-golems/llm-proxy/pkg/byok/tokens"
	"github.com/go-go-golems/llm-proxy/pkg/byok/vault"
)

// masterKeyEnv is the fallback source for the vault master key.
const masterKeyEnv = "LLM_PROXY_BYOK_MASTER_KEY"

func openVault(masterKey string) (*vault.Vault, error) {
	if masterKey == "" {
		masterKey = os.Getenv(masterKeyEnv)
	}
	if masterKey == "" {
		return nil, errors.Errorf("no master key: pass --master-key or set %s (generate one with 'byok keygen')", masterKeyEnv)
	}
	return vault.NewFromBase64(masterKey)
}

// newByokCommand groups the BYOK management CLI: users, tokens, and (in the
// vault commands) credentials. These are operator/dev utilities; the control
// plane webapp is the user-facing path.
func newByokCommand() *cobra.Command {
	var dbPath string

	byokCmd := &cobra.Command{
		Use:   "byok",
		Short: "Manage BYOK users, credentials, and minted tokens",
	}
	byokCmd.PersistentFlags().StringVar(&dbPath, "db", "var/byok.db", "Path to the BYOK SQLite database")

	openStore := func() (store.Store, error) {
		return sqlite.Open(dbPath)
	}

	// --- user commands ---
	userCmd := &cobra.Command{Use: "user", Short: "Manage BYOK users"}

	var username, email, subject string
	userAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Create (or update) a user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if username == "" {
				return errors.New("--username is required")
			}
			if subject == "" {
				subject = "local:" + username
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.UpsertUser(cmd.Context(), store.User{OIDCSubject: subject, Username: username, Email: email})
			if err != nil {
				return err
			}
			cmd.Printf("user %s (id %s, subject %s)\n", u.Username, u.ID, u.OIDCSubject)
			return nil
		},
	}
	userAddCmd.Flags().StringVar(&username, "username", "", "Username")
	userAddCmd.Flags().StringVar(&email, "email", "", "Email address")
	userAddCmd.Flags().StringVar(&subject, "subject", "", "OIDC subject (default local:<username>)")

	userListCmd := &cobra.Command{
		Use:   "list",
		Short: "List users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			users, err := st.ListUsers(cmd.Context())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tUSERNAME\tEMAIL\tSUBJECT")
			for _, u := range users {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.ID, u.Username, u.Email, u.OIDCSubject)
			}
			return tw.Flush()
		},
	}
	userCmd.AddCommand(userAddCmd, userListCmd)

	// --- token commands ---
	tokenCmd := &cobra.Command{Use: "token", Short: "Mint, list, and revoke BYOK tokens"}

	var mintUser, mintName, mintModels, mintCredentials string
	var maxTokens, maxRequests, rpm, expiresDays int64
	tokenMintCmd := &cobra.Command{
		Use:   "mint",
		Short: "Mint a scoped token (the plaintext is printed exactly once)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if mintUser == "" || mintName == "" || mintModels == "" {
				return errors.New("--user, --name, and --models are required")
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.GetUserByUsername(cmd.Context(), mintUser)
			if err != nil {
				return errors.Wrapf(err, "user %q", mintUser)
			}

			var credIDs []string
			if mintCredentials != "" {
				credIDs = splitCSV(mintCredentials)
				for _, id := range credIDs {
					if _, err := st.GetCredential(cmd.Context(), u.ID, id); err != nil {
						return errors.Wrapf(err, "credential %q", id)
					}
				}
			}

			raw, hash, err := tokens.Mint()
			if err != nil {
				return err
			}
			tok := store.Token{
				UserID: u.ID, TokenHash: hash, Name: mintName,
				CredentialIDs: credIDs, AllowedModels: splitCSV(mintModels),
			}
			if maxTokens > 0 {
				tok.MaxTotalTokens = &maxTokens
			}
			if maxRequests > 0 {
				tok.MaxRequests = &maxRequests
			}
			if rpm > 0 {
				tok.RateLimitRPM = &rpm
			}
			if expiresDays > 0 {
				exp := time.Now().UTC().Add(time.Duration(expiresDays) * 24 * time.Hour)
				tok.ExpiresAt = &exp
			}
			minted, err := st.MintToken(cmd.Context(), tok)
			if err != nil {
				return err
			}
			_ = st.AppendEvent(cmd.Context(), store.AuditEvent{
				UserID: u.ID, TokenID: minted.ID, EventType: "token.minted",
				Payload: []byte(fmt.Sprintf(`{"name":%q,"models":%q}`, mintName, mintModels)),
			})
			cmd.Printf("token %s minted for %s (id %s)\n", mintName, u.Username, minted.ID)
			cmd.Printf("THIS IS THE ONLY TIME THE TOKEN IS SHOWN:\n%s\n", raw)
			return nil
		},
	}
	tokenMintCmd.Flags().StringVar(&mintUser, "user", "", "Owner username")
	tokenMintCmd.Flags().StringVar(&mintName, "name", "", "Token label")
	tokenMintCmd.Flags().StringVar(&mintModels, "models", "", "Comma-separated allowed model slugs or globs (e.g. 'sonnet,gpt-*')")
	tokenMintCmd.Flags().StringVar(&mintCredentials, "credentials", "", "Comma-separated credential IDs the token may use")
	tokenMintCmd.Flags().Int64Var(&maxTokens, "max-tokens", 0, "Total token budget (prompt+completion, 0 = unlimited)")
	tokenMintCmd.Flags().Int64Var(&maxRequests, "max-requests", 0, "Request budget (0 = unlimited)")
	tokenMintCmd.Flags().Int64Var(&rpm, "rpm", 0, "Rate limit in requests per minute (0 = unlimited)")
	tokenMintCmd.Flags().Int64Var(&expiresDays, "expires-days", 0, "Expiry in days from now (0 = no expiry)")

	var listUser string
	tokenListCmd := &cobra.Command{
		Use:   "list",
		Short: "List a user's tokens with usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listUser == "" {
				return errors.New("--user is required")
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.GetUserByUsername(cmd.Context(), listUser)
			if err != nil {
				return errors.Wrapf(err, "user %q", listUser)
			}
			toks, err := st.ListTokensByUser(cmd.Context(), u.ID)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tMODELS\tUSED_TOKENS\tUSED_REQS\tSTATUS")
			for _, t := range toks {
				c, err := st.GetCounters(cmd.Context(), t.ID)
				if err != nil {
					return err
				}
				status := "active"
				switch {
				case t.RevokedAt != nil:
					status = "revoked"
				case t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt):
					status = "expired"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n",
					t.ID, t.Name, strings.Join(t.AllowedModels, ","), c.TotalTokens, c.TotalRequests, status)
			}
			return tw.Flush()
		},
	}
	tokenListCmd.Flags().StringVar(&listUser, "user", "", "Owner username")

	var revokeUser, revokeID string
	tokenRevokeCmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if revokeUser == "" || revokeID == "" {
				return errors.New("--user and --id are required")
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.GetUserByUsername(cmd.Context(), revokeUser)
			if err != nil {
				return errors.Wrapf(err, "user %q", revokeUser)
			}
			if err := st.RevokeToken(cmd.Context(), u.ID, revokeID); err != nil {
				return err
			}
			_ = st.AppendEvent(cmd.Context(), store.AuditEvent{
				UserID: u.ID, TokenID: revokeID, EventType: "token.revoked",
			})
			cmd.Printf("token %s revoked\n", revokeID)
			return nil
		},
	}
	tokenRevokeCmd.Flags().StringVar(&revokeUser, "user", "", "Owner username")
	tokenRevokeCmd.Flags().StringVar(&revokeID, "id", "", "Token ID")

	tokenCmd.AddCommand(tokenMintCmd, tokenListCmd, tokenRevokeCmd)

	// --- keygen ---
	keygenCmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate a vault master key (base64)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			key, err := vault.GenerateKeyBase64()
			if err != nil {
				return err
			}
			cmd.Printf("%s\n", key)
			cmd.PrintErrf("store this securely and pass it via --byok-master-key or %s\n", masterKeyEnv)
			return nil
		},
	}

	// --- credential commands ---
	credCmd := &cobra.Command{Use: "credential", Short: "Manage vault credentials (provider API keys)"}
	var masterKey string
	credCmd.PersistentFlags().StringVar(&masterKey, "master-key", "", "Vault master key (base64; default $"+masterKeyEnv+")")

	var credUser, credProvider, credAPIType, credLabel, credSecretEnv string
	credAddCmd := &cobra.Command{
		Use:   "add",
		Short: "Store a provider API key, encrypted at rest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if credUser == "" || credProvider == "" || credAPIType == "" || credSecretEnv == "" {
				return errors.New("--user, --provider, --api-type, and --secret-env are required")
			}
			secret := os.Getenv(credSecretEnv)
			if secret == "" {
				return errors.Errorf("environment variable %s is empty; secrets are read from the environment to keep them out of shell history", credSecretEnv)
			}
			v, err := openVault(masterKey)
			if err != nil {
				return err
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.GetUserByUsername(cmd.Context(), credUser)
			if err != nil {
				return errors.Wrapf(err, "user %q", credUser)
			}
			credID := store.NewID()
			cipherBlob, err := v.Encrypt(credID, []byte(secret))
			if err != nil {
				return err
			}
			label := credLabel
			if label == "" {
				label = credProvider
			}
			cred, err := st.CreateCredential(cmd.Context(), store.Credential{
				ID: credID, UserID: u.ID, Provider: credProvider, APIType: credAPIType,
				Label: label, SecretCipher: cipherBlob, SecretLast4: vault.Last4(secret),
			})
			if err != nil {
				return err
			}
			_ = st.AppendEvent(cmd.Context(), store.AuditEvent{
				UserID: u.ID, EventType: "credential.created",
				Payload: []byte(fmt.Sprintf(`{"credential_id":%q,"provider":%q}`, cred.ID, credProvider)),
			})
			cmd.Printf("credential %s stored for %s (%s, %s)\n", cred.ID, u.Username, credProvider, cred.SecretLast4)
			return nil
		},
	}
	credAddCmd.Flags().StringVar(&credUser, "user", "", "Owner username")
	credAddCmd.Flags().StringVar(&credProvider, "provider", "", "Provider name (anthropic, openai, ...)")
	credAddCmd.Flags().StringVar(&credAPIType, "api-type", "", "Geppetto api-type (claude, openai, ...)")
	credAddCmd.Flags().StringVar(&credLabel, "label", "", "Display label")
	credAddCmd.Flags().StringVar(&credSecretEnv, "secret-env", "", "Name of the environment variable holding the API key")

	var credListUser string
	credListCmd := &cobra.Command{
		Use:   "list",
		Short: "List a user's credentials (never shows secrets)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if credListUser == "" {
				return errors.New("--user is required")
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.GetUserByUsername(cmd.Context(), credListUser)
			if err != nil {
				return errors.Wrapf(err, "user %q", credListUser)
			}
			creds, err := st.ListCredentialsByUser(cmd.Context(), u.ID)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tPROVIDER\tAPI_TYPE\tLABEL\tSECRET\tDISABLED")
			for _, c := range creds {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%t\n", c.ID, c.Provider, c.APIType, c.Label, c.SecretLast4, c.Disabled)
			}
			return tw.Flush()
		},
	}
	credListCmd.Flags().StringVar(&credListUser, "user", "", "Owner username")

	var credRmUser, credRmID string
	credRmCmd := &cobra.Command{
		Use:   "rm",
		Short: "Delete a credential (revokes tokens bound only to it)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if credRmUser == "" || credRmID == "" {
				return errors.New("--user and --id are required")
			}
			st, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			u, err := st.GetUserByUsername(cmd.Context(), credRmUser)
			if err != nil {
				return errors.Wrapf(err, "user %q", credRmUser)
			}
			if err := st.DeleteCredential(cmd.Context(), u.ID, credRmID); err != nil {
				return err
			}
			_ = st.AppendEvent(cmd.Context(), store.AuditEvent{
				UserID: u.ID, EventType: "credential.deleted",
				Payload: []byte(fmt.Sprintf(`{"credential_id":%q}`, credRmID)),
			})
			cmd.Printf("credential %s deleted\n", credRmID)
			return nil
		},
	}
	credRmCmd.Flags().StringVar(&credRmUser, "user", "", "Owner username")
	credRmCmd.Flags().StringVar(&credRmID, "id", "", "Credential ID")

	credCmd.AddCommand(credAddCmd, credListCmd, credRmCmd)
	byokCmd.AddCommand(userCmd, tokenCmd, credCmd, keygenCmd)
	return byokCmd
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
