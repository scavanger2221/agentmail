package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config represents the full configuration file.
type Config struct {
	DefaultAccount string             `toml:"default_account"`
	Accounts       map[string]Account `toml:"accounts"`
}

// Account holds connection details for one email account.
type Account struct {
	Email        string `toml:"email"`
	IMAPHost     string `toml:"imap_host"`
	IMAPPort     int    `toml:"imap_port"`
	SMTPHost     string `toml:"smtp_host"`
	SMTPPort     int    `toml:"smtp_port"`
	AuthMethod   string `toml:"auth_method"` // "oauth2" or "password"
	PasswordFile string `toml:"password_file,omitempty"`
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config", "agentmail", "config.toml")
}

// Load reads and parses the config file.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Accounts: make(map[string]Account)}, nil
		}
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if _, err := toml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Accounts == nil {
		cfg.Accounts = make(map[string]Account)
	}
	return &cfg, nil
}

// ResolveAccount returns the account name to use, defaulting to DefaultAccount or first configured.
func (c *Config) ResolveAccount(requested string) (string, *Account, error) {
	if requested == "" {
		requested = c.DefaultAccount
		if requested == "" {
			for name := range c.Accounts {
				requested = name
				break
			}
		}
	}
	if requested == "" {
		return "", nil, fmt.Errorf("no accounts configured")
	}

	acc, ok := c.Accounts[requested]
	if !ok {
		return "", nil, fmt.Errorf("account %q not found", requested)
	}
	return requested, &acc, nil
}
