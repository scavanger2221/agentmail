package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	imaptypes "github.com/emersion/go-imap/v2"
	"github.com/galkasoft/agentmail/internal/cache"
	"github.com/galkasoft/agentmail/internal/imap"
	"github.com/galkasoft/agentmail/internal/output"
	"github.com/galkasoft/agentmail/internal/smtp"
	"github.com/spf13/cobra"
)

var (
	emailsFolder string
	emailsLimit  int
	emailsOffset int
	searchRemote bool
)

var emailsCmd = &cobra.Command{
	Use:   "emails",
	Short: "Manage emails",
}

var emailsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List emails in a folder",
	Run: func(cmd *cobra.Command, args []string) {
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
		if err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Lookup account: %v", err))
		}
		if accRow == nil {
			err = store.InsertAccount(acctName, acc.Email, acc.IMAPHost, acc.IMAPPort, acc.SMTPHost, acc.SMTPPort, acc.AuthMethod, acc.PasswordFile)
			if err != nil {
				output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Cache account: %v", err))
			}
			accRow, _ = store.GetAccountByName(acctName)
		}

		folder := emailsFolder
		if folder == "" {
			folder = "INBOX"
		}

		folderRow, _ := store.GetFolder(accRow.ID, folder)

		var cached bool
		if folderRow == nil {
			// We don't have folder cached yet, just return empty
			output.SuccessWithMeta(map[string]interface{}{
				"emails": []interface{}{},
				"folder": folder,
				"total":  0,
			}, &output.Meta{
				Account:   acc.Email,
				ElapsedMs: output.Now() - startTime,
				Cached:    false,
			})
			return
		}

		emails, err := store.ListEmails(accRow.ID, folderRow.ID, emailsLimit, emailsOffset)
		if err != nil {
			output.Fatal("INTERNAL_ERROR", fmt.Sprintf("List emails: %v", err))
		}

		cached = len(emails) > 0

		type emailOut struct {
			UID            int    `json:"uid"`
			Subject        string `json:"subject"`
			From           string `json:"from"`
			To             string `json:"to"`
			Date           string `json:"date"`
			Flags          string `json:"flags"`
			Size           int    `json:"size"`
			HasAttachments bool   `json:"has_attachments"`
			Snippet        string `json:"snippet"`
			IsSynced       bool   `json:"is_synced"`
		}

		out := make([]emailOut, 0, len(emails))
		for _, e := range emails {
			out = append(out, emailOut{
				UID:            e.UID,
				Subject:        e.Subject,
				From:           e.FromAddr,
				To:             e.ToAddrs,
				Date:           e.Date,
				Flags:          e.Flags,
				Size:           e.Size,
				HasAttachments: e.HasAttachments,
				Snippet:        e.BodySnippet,
				IsSynced:       e.IsSynced,
			})
		}

		output.SuccessWithMeta(map[string]interface{}{
			"emails": out,
			"folder": folder,
			"total":  len(out),
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
			Cached:    cached,
		})
	},
}

var emailsGetCmd = &cobra.Command{
	Use:   "get <uid>",
	Short: "Get full email by UID",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uidStr := args[0]

		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)
		store := loadCache()
		defer store.Close()

		password := getPassword(acc, acctName)

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		var uidNum uint32
		fmt.Sscanf(uidStr, "%d", &uidNum)

		fullEmail, bodyPath, err := client.FetchFullEmail(imaptypes.UID(uidNum), cache.DataPath())
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Fetch: %v", err))
		}

		// Cache the body text for search
		accRow, _ := store.GetAccountByName(acctName)
		if accRow != nil {
			store.UpdateEmailBodySnippet(accRow.ID, int(uidNum), fullEmail.BodyText)
		}

		type emailFullOut struct {
			UID      int    `json:"uid"`
			Subject  string `json:"subject"`
			From     string `json:"from"`
			To       string `json:"to"`
			Date     string `json:"date"`
			Body     string `json:"body"`
			BodyPath string `json:"body_path"`
		}

		output.SuccessWithMeta(emailFullOut{
			UID:      int(fullEmail.UID),
			Subject:  fullEmail.Subject,
			From:     fullEmail.From,
			To:       fullEmail.To,
			Date:     fullEmail.Date,
			Body:     fullEmail.BodyText,
			BodyPath: bodyPath,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search emails in local cache",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "Search query required")
		}
		query := args[0]

		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc, accRow := resolveAccountStore(cfg, loadCache())
		store := loadCache()
		defer store.Close()

		if searchRemote {
			// Remote IMAP search
			password := getPassword(acc, acctName)
			client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
			if err != nil {
				output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
			}
			defer client.Close()

			uids, err := client.Search(query)
			if err != nil {
				output.Fatal("IMAP_ERROR", fmt.Sprintf("Search: %v", err))
			}

			type searchOut struct {
				UID int `json:"uid"`
			}
			out := make([]searchOut, 0, len(uids))
			for _, u := range uids {
				out = append(out, searchOut{UID: int(u)})
			}

			output.SuccessWithMeta(map[string]interface{}{
				"query":   query,
				"results": out,
				"total":   len(out),
			}, &output.Meta{
				Account:   acc.Email,
				ElapsedMs: output.Now() - startTime,
			})
			return
		}

		results, err := store.SearchEmails(accRow.ID, query, 50)
		if err != nil {
			results, err = store.SearchEmailsSimple(accRow.ID, query, 50)
			if err != nil {
				output.Fatal("INTERNAL_ERROR", fmt.Sprintf("Search: %v", err))
			}
		}

		type searchOut struct {
			UID     int    `json:"uid"`
			Subject string `json:"subject"`
			From    string `json:"from"`
			Snippet string `json:"snippet"`
			Date    string `json:"date"`
		}

		out := make([]searchOut, 0, len(results))
		for _, e := range results {
			out = append(out, searchOut{
				UID:     e.UID,
				Subject: e.Subject,
				From:    e.FromAddr,
				Snippet: e.BodySnippet,
				Date:    e.Date,
			})
		}

		output.SuccessWithMeta(map[string]interface{}{
			"query":   query,
			"results": out,
			"total":   len(out),
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
			Cached:    true,
		})
	},
}

var emailsSendCmd = &cobra.Command{
	Use:   "send",
	Short: "Send an email (reads JSON from stdin)",
	Run: func(cmd *cobra.Command, args []string) {
		startTime := output.Now()

		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Read stdin: %v", err))
		}

		var input struct {
			To          []string `json:"to"`
			Cc          []string `json:"cc"`
			Subject     string   `json:"subject"`
			Body        string   `json:"body"`
			Attachments []string `json:"attachments"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Invalid JSON: %v", err))
		}

		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)
		if password == "" {
			output.Fatal("AUTH_FAILED", "No password configured.")
		}

		smtpClient := smtp.NewClient(acc.SMTPHost, acc.SMTPPort, acc.Email, password)

		msg := &smtp.Message{
			From:        acc.Email,
			To:          input.To,
			Cc:          input.Cc,
			Subject:     input.Subject,
			Body:        input.Body,
			Attachments: input.Attachments,
		}

		if err := smtpClient.Send(msg); err != nil {
			output.Fatal("SMTP_ERROR", fmt.Sprintf("Send: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"sent":    true,
			"to":      input.To,
			"subject": input.Subject,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsReplyCmd = &cobra.Command{
	Use:   "reply <uid>",
	Short: "Reply to an email (reads body from stdin)",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uid := args[0]

		startTime := output.Now()

		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Read stdin: %v", err))
		}

		var input struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Invalid JSON: %v", err))
		}

		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)

		smtpClient := smtp.NewClient(acc.SMTPHost, acc.SMTPPort, acc.Email, password)

		msg := &smtp.Message{
			From:    acc.Email,
			Subject: "Re: ",
			Body:    input.Body,
		}

		if err := smtpClient.Send(msg); err != nil {
			output.Fatal("SMTP_ERROR", fmt.Sprintf("Send: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"sent":        true,
			"in_reply_to": uid,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsTrashCmd = &cobra.Command{
	Use:   "trash <uid>",
	Short: "Move email to trash",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uidStr := args[0]

		startTime := output.Now()
		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)

		password := getPassword(acc, acctName)

		client, err := imap.Connect(acc.IMAPHost, acc.IMAPPort, acc.Email, password)
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Connect: %v", err))
		}
		defer client.Close()

		var uidNum uint32
		fmt.Sscanf(uidStr, "%d", &uidNum)

		if err := client.MoveToTrash(imaptypes.UID(uidNum)); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Trash: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"trashed": uidNum,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

func init() {
	emailsListCmd.Flags().StringVar(&emailsFolder, "folder", "INBOX", "IMAP folder to list")
	emailsListCmd.Flags().IntVar(&emailsLimit, "limit", 20, "Max results")
	emailsListCmd.Flags().IntVar(&emailsOffset, "offset", 0, "Offset for pagination")
	emailsSearchCmd.Flags().BoolVar(&searchRemote, "remote", false, "Search IMAP server directly instead of local cache")

	emailsCmd.AddCommand(emailsListCmd)
	emailsCmd.AddCommand(emailsGetCmd)
	emailsCmd.AddCommand(emailsSearchCmd)
	emailsCmd.AddCommand(emailsSendCmd)
	emailsCmd.AddCommand(emailsReplyCmd)
	emailsCmd.AddCommand(emailsTrashCmd)
}
