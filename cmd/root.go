package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/galkasoft/agentmail/internal/cache"
	"github.com/galkasoft/agentmail/internal/config"
	"github.com/galkasoft/agentmail/internal/oauth2"
	"github.com/galkasoft/agentmail/internal/output"
	"github.com/spf13/cobra"
)

var cfgFile string
var outputFormat string
var accountName string

var rootCmd = &cobra.Command{
	Use:   "agentmail",
	Short: "Agent-first CLI email client",
	Long:  `agentmail is a CLI email client designed for AI agent consumption.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if outputFormat == "text" {
			output.FormatText = true
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/agentmail/config.toml)")
	rootCmd.PersistentFlags().StringVar(&outputFormat, "format", "json", "output format: json or text")
	rootCmd.PersistentFlags().StringVar(&accountName, "account", "", "account name to use")

	rootCmd.AddCommand(accountsCmd)
	rootCmd.AddCommand(foldersCmd)
	rootCmd.AddCommand(emailsCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(cacheCmd)
}

func loadConfig() *config.Config {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		output.Fatal("CONFIG_ERROR", fmt.Sprintf("Failed to load config: %v", err))
	}
	return cfg
}

func loadCache() *cache.Store {
	store, err := cache.Open("")
	if err != nil {
		output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Failed to open cache: %v", err))
	}
	return store
}

func resolveAccount(cfg *config.Config) (string, *config.Account) {
	name, acc, err := cfg.ResolveAccount(accountName)
	if err != nil {
		output.Fatal("CONFIG_ERROR", err.Error())
	}
	return name, acc
}

func resolveAccountStore(cfg *config.Config, store *cache.Store) (string, *config.Account, *cache.AccountRow) {
	name, acc := resolveAccount(cfg)
	row, err := store.GetAccountByName(name)
	if err != nil {
		output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Failed to look up account: %v", err))
	}
	if row == nil {
		output.Fatal("CONFIG_ERROR", fmt.Sprintf("Account %q not found in cache. Run 'agentmail accounts sync' first.", name))
	}
	return name, acc, row
}

func getPassword(acc *config.Account, accountName string) string {
	if acc.AuthMethod == "oauth2" {
		return getOAuth2Password(accountName, acc.Email)
	}

	if acc.PasswordFile != "" {
		path := acc.PasswordFile
		if path[0] == '~' {
			home, _ := os.UserHomeDir()
			path = home + path[1:]
		}
		data, err := os.ReadFile(path)
		if err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Failed to read password file: %v", err))
		}
		return string(data)
	}
	return os.Getenv("AGENTMAIL_PASSWORD")
}

func getOAuth2Password(accountName, email string) string {
	token, err := oauth2.LoadToken(accountName)
	if err != nil {
		output.Fatal("AUTH_FAILED", fmt.Sprintf("No OAuth2 token found for %q. Run 'agentmail accounts add %s' first.", accountName, accountName))
	}

	if !token.Valid() {
		cfg := oauth2.GetGmailOAuthConfig()
		if cfg == nil {
			output.Fatal("AUTH_FAILED", "OAuth2 token expired and no client ID/secret configured for refresh.")
		}
		tokenSource := cfg.TokenSource(context.Background(), token)
		newToken, err := tokenSource.Token()
		if err != nil {
			output.Fatal("AUTH_FAILED", fmt.Sprintf("Failed to refresh token: %v", err))
		}
		if err := oauth2.SaveToken(accountName, newToken); err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Failed to save refreshed token: %v", err))
		}
		token = newToken
	}

	return fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", email, token.AccessToken)
}
