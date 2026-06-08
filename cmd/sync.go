package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/galkasoft/agentmail/internal/cache"
	"github.com/galkasoft/agentmail/internal/imap"
	"github.com/galkasoft/agentmail/internal/output"
	"github.com/spf13/cobra"
)

var (
	syncBg      bool
	syncFolders string
	syncBgExec  bool
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Force sync from IMAP to local cache",
	Run: func(cmd *cobra.Command, args []string) {
		// If --bg is set, re-execute ourselves in background
		if syncBg && !syncBgExec {
			exe, err := os.Executable()
			if err != nil {
				output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Find executable: %v", err))
			}
			bgCmd := exec.Command(exe, "sync", "--bg-exec")
			if cfgFile != "" {
				bgCmd.Args = append(bgCmd.Args, "--config", cfgFile)
			}
			if accountName != "" {
				bgCmd.Args = append(bgCmd.Args, "--account", accountName)
			}
			bgCmd.Stdout = os.Stdout
			bgCmd.Stderr = os.Stderr
			if err := bgCmd.Start(); err != nil {
				output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Start background sync: %v", err))
			}
			output.Success(map[string]interface{}{
				"message": "Sync started in background",
				"pid":     bgCmd.Process.Pid,
			})
			return
		}
		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)
		store := loadCache()
		defer store.Close()

		password := getPassword(acc, acctName)
		if password == "" {
			output.Fatal("AUTH_FAILED", "No password configured.")
		}

		accRow, err := store.GetAccountByName(acctName)
		if err != nil || accRow == nil {
			err = store.InsertAccount(acctName, acc.Email, acc.IMAPHost, acc.IMAPPort, acc.SMTPHost, acc.SMTPPort, acc.AuthMethod, acc.PasswordFile)
			if err != nil {
				output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Cache account: %v", err))
			}
			accRow, _ = store.GetAccountByName(acctName)
		}

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		folders, err := client.ListFolders()
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("List folders: %v", err))
		}

		// Filter folders if --folders flag is set
		if syncFolders != "" {
			wanted := strings.Split(syncFolders, ",")
			var filtered []imap.FolderInfo
			for _, f := range folders {
				for _, w := range wanted {
					if strings.EqualFold(strings.TrimSpace(w), f.Name) {
						filtered = append(filtered, f)
						break
					}
				}
			}
			folders = filtered
		}

		totalSynced := 0
		errored := 0

		for _, f := range folders {
			// Skip non-selectable folders
			if strings.HasPrefix(f.Name, "[Gmail]") && f.Name != "[Gmail]/Trash" {
				continue
			}

			uidValidity, err := client.Select(f.Name)
			if err != nil {
				errored++
				continue
			}

			folderRow, err := store.GetFolder(accRow.ID, f.Name)
			if folderRow == nil {
				folderID, err := store.UpsertFolder(accRow.ID, f.Name, int(uidValidity))
				if err != nil {
					errored++
					continue
				}
				folderRow = &cache.FolderRow{ID: folderID, UIDValidity: int(uidValidity), LastSyncUID: 0}
			} else if folderRow.UIDValidity != int(uidValidity) {
				store.UpdateFolderSync(folderRow.ID, int(uidValidity), 0)
				folderRow.UIDValidity = int(uidValidity)
				folderRow.LastSyncUID = 0
			}

			newUIDs, err := client.FetchNewUIDs(uint32(folderRow.LastSyncUID))
			if err != nil {
				errored++
				continue
			}

			if len(newUIDs) > 0 {
				// Fetch in batches
				batchSize := 200
				for i := 0; i < len(newUIDs); i += batchSize {
					end := i + batchSize
					if end > len(newUIDs) {
						end = len(newUIDs)
					}
					batch := newUIDs[i:end]

					summaries, err := client.FetchEmailSummaries(batch)
					if err != nil {
						errored++
						break
					}

					for _, s := range summaries {
						err := store.InsertEmail(
							accRow.ID, folderRow.ID, int(s.UID),
							s.MessageID, s.Subject, s.From, s.To,
							s.Date, s.Flags, s.BodySnippet,
							int(s.Size), s.HasAttachments, s.InternalDate,
						)
						if err != nil {
							continue
						}
						totalSynced++
					}
				}

				if len(newUIDs) > 0 {
					lastUID := uint32(newUIDs[len(newUIDs)-1])
					store.UpdateFolderSync(folderRow.ID, int(uidValidity), int(lastUID))
				}
			}
		}

		output.SuccessWithMeta(map[string]interface{}{
			"synced":          totalSynced,
			"folders_scanned": len(folders),
			"folders_errored": errored,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

func init() {
	syncCmd.Flags().BoolVar(&syncBg, "bg", false, "Run sync in background (returns immediately)")
	syncCmd.Flags().BoolVar(&syncBgExec, "bg-exec", false, "Internal use: actual background sync execution")
	syncCmd.Flags().MarkHidden("bg-exec")
	syncCmd.Flags().StringVar(&syncFolders, "folders", "", "Comma-separated list of folders to sync (default: all, excluding [Gmail]/*)")
}
