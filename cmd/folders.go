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

var foldersCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new IMAP folder",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "Folder name required")
		}
		name := args[0]

		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		if err := client.CreateFolder(name); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Create folder: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"created": name,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var foldersDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an IMAP folder",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "Folder name required")
		}
		name := args[0]

		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		if err := client.DeleteFolder(name); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Delete folder: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"deleted": name,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var foldersRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename an IMAP folder",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			output.Fatal("CONFIG_ERROR", "Old and new folder names required")
		}
		oldName := args[0]
		newName := args[1]

		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		if err := client.RenameFolder(oldName, newName); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Rename folder: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"renamed": oldName,
			"to":      newName,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

func init() {
	foldersCmd.AddCommand(foldersListCmd)
	foldersCmd.AddCommand(foldersCreateCmd)
	foldersCmd.AddCommand(foldersDeleteCmd)
	foldersCmd.AddCommand(foldersRenameCmd)
}
