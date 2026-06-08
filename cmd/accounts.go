package cmd

import (
	"fmt"

	"github.com/galkasoft/agentmail/internal/oauth2"
	"github.com/galkasoft/agentmail/internal/output"
	"github.com/spf13/cobra"
)

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "Manage email accounts",
}

var accountsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured accounts",
	Run: func(cmd *cobra.Command, args []string) {
		store := loadCache()
		defer store.Close()

		rows, err := store.ListAccounts()
		if err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Failed to list accounts: %v", err))
		}

		type accountOut struct {
			Name       string `json:"name"`
			Email      string `json:"email"`
			IMAPHost   string `json:"imap_host"`
			IMAPPort   int    `json:"imap_port"`
			SMTPHost   string `json:"smtp_host"`
			SMTPPort   int    `json:"smtp_port"`
			AuthMethod string `json:"auth_method"`
		}

		accounts := make([]accountOut, 0, len(rows))
		for _, r := range rows {
			accounts = append(accounts, accountOut{
				Name:       r.Name,
				Email:      r.Email,
				IMAPHost:   r.IMAPHost,
				IMAPPort:   r.IMAPPort,
				SMTPHost:   r.SMTPHost,
				SMTPPort:   r.SMTPPort,
				AuthMethod: r.AuthMethod,
			})
		}

		output.Success(map[string]interface{}{"accounts": accounts})
	},
}

var accountsAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new email account",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "account name required")
		}
		name := args[0]

		cfg := loadConfig()
		acc, ok := cfg.Accounts[name]
		if !ok {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Account %q not found in config file. Add it to the config first.", name))
		}

		store := loadCache()
		defer store.Close()

		// Run OAuth2 flow if needed
		if acc.AuthMethod == "oauth2" {
			oauthCfg := oauth2.GetGmailOAuthConfig()
			if oauthCfg == nil {
				output.Fatal("CONFIG_ERROR", "OAuth2 requires AGENTMAIL_GMAIL_CLIENT_ID and AGENTMAIL_GMAIL_CLIENT_SECRET env vars.")
			}
			token, err := oauth2.Authorize(oauthCfg)
			if err != nil {
				output.Fatal("AUTH_FAILED", fmt.Sprintf("OAuth2 flow failed: %v", err))
			}
			if err := oauth2.SaveToken(name, token); err != nil {
				output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Save token: %v", err))
			}
		}

		err := store.InsertAccount(name, acc.Email, acc.IMAPHost, acc.IMAPPort, acc.SMTPHost, acc.SMTPPort, acc.AuthMethod, acc.PasswordFile)
		if err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Failed to add account: %v", err))
		}

		output.Success(map[string]interface{}{"added": name})
	},
}

var accountsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an account",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "account name required")
		}
		name := args[0]

		store := loadCache()
		defer store.Close()

		err := store.RemoveAccount(name)
		if err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Failed to remove account: %v", err))
		}

		output.Success(map[string]interface{}{"removed": name})
	},
}

func init() {
	accountsCmd.AddCommand(accountsListCmd)
	accountsCmd.AddCommand(accountsAddCmd)
	accountsCmd.AddCommand(accountsRemoveCmd)
}
