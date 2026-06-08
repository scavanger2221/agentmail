package cmd

import (
	"fmt"

	imaptypes "github.com/emersion/go-imap/v2"
	"github.com/galkasoft/agentmail/internal/imap"
	"github.com/galkasoft/agentmail/internal/output"
	"github.com/spf13/cobra"
)

var (
	fetchBodiesLimit int
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Cache management",
}

var cacheStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cache statistics",
	Run: func(cmd *cobra.Command, args []string) {
		store := loadCache()
		defer store.Close()

		emails, _ := store.CountEmails()
		folders, _ := store.CountFolders()
		accounts, _ := store.CountAccounts()

		output.Success(map[string]interface{}{
			"emails":   emails,
			"folders":  folders,
			"accounts": accounts,
		})
	},
}

var cacheFetchBodiesCmd = &cobra.Command{
	Use:   "fetch-bodies",
	Short: "Fetch body text for emails missing it",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)
		store := loadCache()
		defer store.Close()

		accRow, err := store.GetAccountByName(acctName)
		if err != nil || accRow == nil {
			output.Fatal("CONFIG_ERROR", "Account not found in cache. Run sync first.")
		}

		uids, err := store.GetEmailsWithoutBodies(accRow.ID, fetchBodiesLimit)
		if err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Query: %v", err))
		}

		if len(uids) == 0 {
			output.Success(map[string]interface{}{"fetched": 0, "message": "All emails have body text"})
			return
		}

		password := getPassword(acc, acctName)
		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		// Fetch body previews in batches
		fetched := 0
		batchSize := 50
		for i := 0; i < len(uids); i += batchSize {
			end := i + batchSize
			if end > len(uids) {
				end = len(uids)
			}
			batch := uids[i:end]

			imapUIDs := make([]imaptypes.UID, len(batch))
			for j, u := range batch {
				imapUIDs[j] = imaptypes.UID(u)
			}

			previews, err := client.FetchBodyPreviews(imapUIDs)
			if err != nil {
				output.Fatal("IMAP_ERROR", fmt.Sprintf("Fetch: %v", err))
			}

			for uid, snippet := range previews {
				store.UpdateEmailBodySnippet(accRow.ID, uid, snippet)
				fetched++
			}
		}

		output.Success(map[string]interface{}{
			"fetched": fetched,
			"total_missing": len(uids),
		})
	},
}

func init() {
	cacheFetchBodiesCmd.Flags().IntVar(&fetchBodiesLimit, "limit", 100, "Max emails to fetch bodies for")
	cacheCmd.AddCommand(cacheStatusCmd)
	cacheCmd.AddCommand(cacheFetchBodiesCmd)
}
