package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

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
	trashSource  string
	moveDest     string
	moveSource   string
	copyDest     string
	flagAdd      bool
	flagRemove   bool
	flagNames    string
	deleteSource string
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

var getEmailFolder string

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

		folder := getEmailFolder
		if folder == "" {
			folder = "INBOX"
		}

		if _, err := client.Select(folder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", folder, err))
		}

		var uidNum uint32
		fmt.Sscanf(uidStr, "%d", &uidNum)

		fullEmail, bodyPath, err := client.FetchFullEmail(imaptypes.UID(uidNum), cache.DataPath())
		if err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Fetch: %v", err))
		}

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
	Short: "Reply to an email (reads JSON from stdin)",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uidStr := args[0]

		startTime := output.Now()

		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Read stdin: %v", err))
		}

		var input struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if err := json.Unmarshal(data, &input); err != nil {
			output.Fatal("CONFIG_ERROR", fmt.Sprintf("Invalid JSON: %v", err))
		}

		cfg := loadConfig()
		acctName, acc := resolveAccount(cfg)
		store := loadCache()
		defer store.Close()

		password := getPassword(acc, acctName)

		accRow, _ := store.GetAccountByName(acctName)

		var originalSubject, originalFrom, messageID string

		if accRow != nil {
			var uidNum int
			fmt.Sscanf(uidStr, "%d", &uidNum)

			folders, _ := store.ListFolders(accRow.ID)
			for _, f := range folders {
				emailRow, err := store.GetEmailByUID(accRow.ID, f.ID, uidNum)
				if err != nil || emailRow == nil {
					continue
				}
				originalSubject = emailRow.Subject
				originalFrom = emailRow.FromAddr
				messageID = emailRow.MessageID
				break
			}
		}

		replyTo := input.To
		if replyTo == "" {
			replyTo = originalFrom
		}

		subject := input.Subject
		if subject == "" {
			if originalSubject != "" && !strings.HasPrefix(originalSubject, "Re: ") && !strings.HasPrefix(strings.ToLower(originalSubject), "re: ") {
				subject = "Re: " + originalSubject
			} else if originalSubject != "" {
				subject = originalSubject
			}
		}

		if replyTo == "" {
			output.Fatal("CONFIG_ERROR", "Could not resolve reply-to address. Provide 'to' field in JSON input.")
		}

		smtpClient := smtp.NewClient(acc.SMTPHost, acc.SMTPPort, acc.Email, password)

		msg := &smtp.Message{
			From:       acc.Email,
			To:         []string{replyTo},
			Subject:    subject,
			Body:       input.Body,
			InReplyTo:  messageID,
			References: messageID,
		}

		if err := smtpClient.Send(msg); err != nil {
			output.Fatal("SMTP_ERROR", fmt.Sprintf("Send: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"sent":        true,
			"in_reply_to": uidStr,
			"to":          replyTo,
			"subject":     subject,
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

		sourceFolder := trashSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		if err := client.MoveToTrash(imaptypes.UID(uidNum)); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Trash: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"trashed": uidNum,
			"folder":  sourceFolder,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsMoveCmd = &cobra.Command{
	Use:   "move <uid>",
	Short: "Move email to another folder",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uidStr := args[0]

		if moveDest == "" {
			output.Fatal("CONFIG_ERROR", "--dest folder required")
		}

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

		sourceFolder := moveSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		if err := client.Move(imaptypes.UID(uidNum), moveDest); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Move: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"moved": uidNum,
			"from":  sourceFolder,
			"to":    moveDest,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsCopyCmd = &cobra.Command{
	Use:   "copy <uid>",
	Short: "Copy email to another folder",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uidStr := args[0]

		if copyDest == "" {
			output.Fatal("CONFIG_ERROR", "--dest folder required")
		}

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

		sourceFolder := moveSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		if err := client.Copy(imaptypes.UID(uidNum), copyDest); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Copy: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"copied": uidNum,
			"to":     copyDest,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsFlagCmd = &cobra.Command{
	Use:   "flag <uid>",
	Short: "Add or remove flags on an email",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			output.Fatal("CONFIG_ERROR", "UID required")
		}
		uidStr := args[0]

		if flagNames == "" {
			output.Fatal("CONFIG_ERROR", "--flags required (comma-separated, e.g. \"\\\\Flagged,\\\\Seen\")")
		}
		if !flagAdd && !flagRemove {
			output.Fatal("CONFIG_ERROR", "Specify --add or --remove")
		}

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

		sourceFolder := moveSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		var flagList []imaptypes.Flag
		for _, f := range strings.Split(flagNames, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				flagList = append(flagList, imaptypes.Flag(f))
			}
		}

		if err := client.SetFlags(imaptypes.UID(uidNum), flagAdd, flagList); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Flag: %v", err))
		}

		action := "added"
		if flagRemove {
			action = "removed"
		}

		output.SuccessWithMeta(map[string]interface{}{
			"uid":    uidNum,
			"action": action,
			"flags":  flagNames,
			"folder": sourceFolder,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsReadCmd = &cobra.Command{
	Use:   "read <uid>",
	Short: "Mark email as read (add \\Seen flag)",
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

		sourceFolder := moveSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		if err := client.SetFlags(imaptypes.UID(uidNum), true, []imaptypes.Flag{imaptypes.FlagSeen}); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Mark read: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"uid":    uidNum,
			"marked": "read",
			"folder": sourceFolder,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsUnreadCmd = &cobra.Command{
	Use:   "unread <uid>",
	Short: "Mark email as unread (remove \\Seen flag)",
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

		sourceFolder := moveSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		if err := client.SetFlags(imaptypes.UID(uidNum), false, []imaptypes.Flag{imaptypes.FlagSeen}); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Mark unread: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"uid":    uidNum,
			"marked": "unread",
			"folder": sourceFolder,
		}, &output.Meta{
			Account:   acc.Email,
			ElapsedMs: output.Now() - startTime,
		})
	},
}

var emailsDeleteCmd = &cobra.Command{
	Use:   "delete <uid>",
	Short: "Permanently delete an email",
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

		sourceFolder := deleteSource
		if sourceFolder == "" {
			sourceFolder = "INBOX"
		}

		if _, err := client.Select(sourceFolder); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Select folder %q: %v", sourceFolder, err))
		}

		if err := client.SetFlags(imaptypes.UID(uidNum), true, []imaptypes.Flag{imaptypes.FlagDeleted}); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Mark deleted: %v", err))
		}

		if err := client.ExpungeMessages(); err != nil {
			output.Fatal("IMAP_ERROR", fmt.Sprintf("Expunge: %v", err))
		}

		output.SuccessWithMeta(map[string]interface{}{
			"deleted": uidNum,
			"folder":  sourceFolder,
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
	emailsGetCmd.Flags().StringVar(&getEmailFolder, "folder", "INBOX", "IMAP folder the email is in")
	emailsSearchCmd.Flags().BoolVar(&searchRemote, "remote", false, "Search IMAP server directly instead of local cache")
	emailsTrashCmd.Flags().StringVar(&trashSource, "folder", "INBOX", "Source folder the email is in")
	emailsMoveCmd.Flags().StringVar(&moveDest, "dest", "", "Destination folder")
	emailsMoveCmd.Flags().StringVar(&moveSource, "folder", "INBOX", "Source folder the email is in")
	emailsCopyCmd.Flags().StringVar(&copyDest, "dest", "", "Destination folder")
	emailsCopyCmd.Flags().StringVar(&moveSource, "folder", "INBOX", "Source folder the email is in")
	emailsFlagCmd.Flags().BoolVar(&flagAdd, "add", false, "Add flags")
	emailsFlagCmd.Flags().BoolVar(&flagRemove, "remove", false, "Remove flags")
	emailsFlagCmd.Flags().StringVar(&flagNames, "flags", "", "Comma-separated flags (e.g. \"\\\\Flagged,\\\\Seen\")")
	emailsFlagCmd.Flags().StringVar(&moveSource, "folder", "INBOX", "Source folder the email is in")
	emailsReadCmd.Flags().StringVar(&moveSource, "folder", "INBOX", "Source folder the email is in")
	emailsUnreadCmd.Flags().StringVar(&moveSource, "folder", "INBOX", "Source folder the email is in")
	emailsDeleteCmd.Flags().StringVar(&deleteSource, "folder", "INBOX", "Source folder the email is in")

	emailsCmd.AddCommand(emailsListCmd)
	emailsCmd.AddCommand(emailsGetCmd)
	emailsCmd.AddCommand(emailsSearchCmd)
	emailsCmd.AddCommand(emailsSendCmd)
	emailsCmd.AddCommand(emailsReplyCmd)
	emailsCmd.AddCommand(emailsTrashCmd)
	emailsCmd.AddCommand(emailsMoveCmd)
	emailsCmd.AddCommand(emailsCopyCmd)
	emailsCmd.AddCommand(emailsFlagCmd)
	emailsCmd.AddCommand(emailsReadCmd)
	emailsCmd.AddCommand(emailsUnreadCmd)
	emailsCmd.AddCommand(emailsDeleteCmd)
}
