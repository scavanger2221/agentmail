package cmd

import (
	"fmt"

	"github.com/galkasoft/agentmail/internal/imap"
	"github.com/galkasoft/agentmail/internal/output"
	"github.com/spf13/cobra"
)

var foldersCmd = &cobra.Command{
	Use:   "folders",
	Short: "Manage email folders",
}

var foldersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List IMAP folders/mailboxes",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)
		if password == "" {
			output.Fatal("AUTH_FAILED", "No password configured. Set AGENTMAIL_PASSWORD or password_file in config.")
		}

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Failed to connect: %v", err))
		}
		defer client.Close()

		folders, err := client.ListFolders()
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Failed to list folders: %v", err))
		}

		type folderOut struct {
			Name      string `json:"name"`
			Delimiter string `json:"delimiter"`
		}
		out := make([]folderOut, 0, len(folders))
		for _, f := range folders {
			out = append(out, folderOut{Name: f.Name, Delimiter: f.Delimiter})
		}

		output.Success(map[string]interface{}{"folders": out})
	},
}

func init() {
	foldersCmd.AddCommand(foldersListCmd)
}
