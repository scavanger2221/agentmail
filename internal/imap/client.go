package imap

import (
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// FolderInfo represents an IMAP folder.
type FolderInfo struct {
	Name      string
	Delimiter string
}

// EmailSummary holds basic email metadata.
type EmailSummary struct {
	UID            imap.UID
	Subject        string
	From           string
	To             string
	Date           string
	Flags          string
	Size           int64
	MessageID      string
	InternalDate   string
	HasAttachments bool
	BodySnippet    string
}

// EmailFull holds complete email data.
type EmailFull struct {
	EmailSummary
	BodyText    string
	BodyHTML    string
	Attachments []AttachmentInfo
}

// AttachmentInfo describes an attachment.
type AttachmentInfo struct {
	Filename    string
	MimeType    string
	Size        int
	StoragePath string
}

// Client wraps an IMAP connection.
type Client struct {
	host     string
	port     int
	username string
	password string
	imap     *imapclient.Client
}

// Connect establishes a TLS IMAP connection and logs in.
func Connect(host string, port int, username, password string) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return nil, fmt.Errorf("tls dial: %w", err)
	}

	c := imapclient.New(conn, &imapclient.Options{})

	if err := c.Login(username, password).Wait(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("login: %w", err)
	}

	return &Client{
		host:     host,
		port:     port,
		username: username,
		password: password,
		imap:     c,
	}, nil
}

// Close closes the IMAP connection.
func (c *Client) Close() error {
	if c.imap == nil {
		return nil
	}
	return c.imap.Close()
}

// ListFolders returns all available folders.
func (c *Client) ListFolders() ([]FolderInfo, error) {
	mailboxes, err := c.imap.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}

	var folders []FolderInfo
	for _, m := range mailboxes {
		if !hasMailboxAttr(m.Attrs, imap.MailboxAttrNoSelect) {
			folders = append(folders, FolderInfo{
				Name:      m.Mailbox,
				Delimiter: fmt.Sprintf("%c", m.Delim),
			})
		}
	}
	return folders, nil
}

func hasMailboxAttr(attrs []imap.MailboxAttr, attr imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == attr {
			return true
		}
	}
	return false
}

// Select opens a folder and returns its UID validity.
func (c *Client) Select(folder string) (uint32, error) {
	status, err := c.imap.Select(folder, nil).Wait()
	if err != nil {
		return 0, fmt.Errorf("select %q: %w", folder, err)
	}
	return status.UIDValidity, nil
}

// FetchNewUIDs returns UIDs greater than lastUID in the selected folder.
func (c *Client) FetchNewUIDs(lastUID uint32) ([]imap.UID, error) {
	searchData, err := c.imap.UIDSearch(&imap.SearchCriteria{}, &imap.SearchOptions{}).Wait()
	if err != nil {
		return nil, fmt.Errorf("uid search: %w", err)
	}

	uids := searchData.AllUIDs()

	if lastUID > 0 {
		var filtered []imap.UID
		for _, u := range uids {
			if uint32(u) > lastUID {
				filtered = append(filtered, u)
			}
		}
		return filtered, nil
	}
	return uids, nil
}

// Search performs a remote IMAP SEARCH (body text) and returns matching UIDs.
// Selects INBOX first since IMAP requires a selected folder for SEARCH.
func (c *Client) Search(query string) ([]imap.UID, error) {
	// Select INBOX first - required for SEARCH to work
	if _, err := c.imap.Select("INBOX", nil).Wait(); err != nil {
		return nil, fmt.Errorf("select INBOX: %w", err)
	}

	criteria := &imap.SearchCriteria{
		Text: []string{query},
	}
	searchData, err := c.imap.UIDSearch(criteria, &imap.SearchOptions{}).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return searchData.AllUIDs(), nil
}

// FetchBodyPreviews fetches text/plain body previews for a batch of UIDs.
// Returns a map of UID → body text snippet.
func (c *Client) FetchBodyPreviews(uids []imap.UID) (map[int]string, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	// Must select a folder first
	if _, err := c.imap.Select("INBOX", nil).Wait(); err != nil {
		return nil, fmt.Errorf("select INBOX: %w", err)
	}

	seqSet := imap.UIDSet{}
	for _, uid := range uids {
		seqSet.AddNum(uid)
	}

	textSection := &imap.FetchItemBodySection{
		Specifier: imap.PartSpecifierText,
		Peek:      true,
		Partial:   &imap.SectionPartial{Offset: 0, Size: 4096},
	}

	fetchOpts := &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{textSection},
	}

	messages, err := c.imap.Fetch(seqSet, fetchOpts).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	result := make(map[int]string, len(messages))
	for _, buf := range messages {
		snippet := ""
		if data := buf.FindBodySection(textSection); data != nil {
			snippet = sanitize(string(data))
		}
		result[int(buf.UID)] = snippet
	}

	return result, nil
}

func sanitize(raw string) string {
	// Quick cleanup: collapse whitespace, remove quoted-printable artifacts
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := strings.Split(raw, "\n")
	var out strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Content-") || strings.HasPrefix(line, "--") {
			continue
		}
		if strings.Contains(line, "=") && !strings.Contains(line, " ") && len(line) > 40 {
			continue // likely qp/base64
		}
		out.WriteString(line)
		out.WriteString(" ")
	}
	return strings.TrimSpace(out.String())
}

// FetchEmailSummaries fetches metadata for a list of UIDs.
func (c *Client) FetchEmailSummaries(uids []imap.UID) ([]EmailSummary, error) {
	if len(uids) == 0 {
		return nil, nil
	}

	seqSet := imap.UIDSet{}
	for _, uid := range uids {
		seqSet.AddNum(uid)
	}

	fetchOpts := &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: false},
	}

	messages, err := c.imap.Fetch(seqSet, fetchOpts).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	var summaries []EmailSummary
	for _, buf := range messages {
		s := EmailSummary{
			UID:  buf.UID,
			Size: buf.RFC822Size,
		}

		if !buf.InternalDate.IsZero() {
			s.InternalDate = buf.InternalDate.Format(time.RFC3339)
		}

		var flags []string
		for _, f := range buf.Flags {
			flags = append(flags, string(f))
		}
		s.Flags = strings.Join(flags, " ")

		if buf.Envelope != nil {
			s.Subject = buf.Envelope.Subject
			if len(buf.Envelope.From) > 0 {
				s.From = buf.Envelope.From[0].Addr()
			}
			if len(buf.Envelope.To) > 0 {
				s.To = buf.Envelope.To[0].Addr()
			}
			s.MessageID = buf.Envelope.MessageID
			if !buf.Envelope.Date.IsZero() {
				s.Date = buf.Envelope.Date.Format(time.RFC3339)
			}
		}

		if buf.BodyStructure != nil {
			s.HasAttachments = hasAttachments(buf.BodyStructure)
		}

		s.BodySnippet = firstN(s.Subject, 200)

		summaries = append(summaries, s)
	}

	return summaries, nil
}

// FetchFullEmail downloads the full RFC822 message and saves it.
func (c *Client) FetchFullEmail(uid imap.UID, dataDir string) (*EmailFull, string, error) {
	seqSet := imap.UIDSet{}
	seqSet.AddNum(uid)

	fetchOpts := &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{
			{},
		},
	}

	messages, err := c.imap.Fetch(seqSet, fetchOpts).Collect()
	if err != nil {
		return nil, "", fmt.Errorf("fetch full: %w", err)
	}

	if len(messages) == 0 {
		return nil, "", fmt.Errorf("no message found for UID %d", uid)
	}

	buf := messages[0]

	var fullBody string
	uidDir := filepath.Join(dataDir, fmt.Sprintf("%d", uid))
	bodyPath := filepath.Join(uidDir, "body.eml")

	// Extract body from sections
	section := &imap.FetchItemBodySection{}
	if data := buf.FindBodySection(section); data != nil {
		fullBody = string(data)
	}

	if fullBody == "" {
		return nil, "", fmt.Errorf("empty body")
	}

	if err := os.MkdirAll(uidDir, 0755); err != nil {
		return nil, "", fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(bodyPath, []byte(fullBody), 0644); err != nil {
		return nil, "", fmt.Errorf("write body: %w", err)
	}

	result := &EmailFull{
		EmailSummary: EmailSummary{
			UID: buf.UID,
		},
		BodyText: extractPlainText(fullBody),
	}

	return result, bodyPath, nil
}

func extractPlainText(body string) string {
	lines := strings.Split(body, "\n")
	inPlain := false
	var result strings.Builder

	for _, line := range lines {
		l := strings.TrimRight(line, "\r")
		if strings.HasPrefix(l, "Content-Type: text/plain") {
			inPlain = true
			continue
		}
		if strings.HasPrefix(l, "Content-Type:") && !strings.Contains(l, "text/plain") {
			inPlain = false
		}
		if inPlain {
			result.WriteString(l)
			result.WriteString("\n")
		}
	}

	return strings.TrimSpace(result.String())
}

// MoveToTrash moves an email to the Trash folder.
func (c *Client) MoveToTrash(uid imap.UID) error {
	seqSet := imap.UIDSet{}
	seqSet.AddNum(uid)

	trashFolder := findTrashFolder(c)
	if trashFolder != "" {
		_, err := c.imap.Move(seqSet, trashFolder).Wait()
		return err
	}

	// Fallback: mark as deleted and expunge
	storeCmd := c.imap.Store(seqSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}, nil)
	if err := storeCmd.Close(); err != nil {
		return err
	}

	return c.imap.Expunge().Close()
}

func findTrashFolder(c *Client) string {
	mailboxes, err := c.imap.List("", "*", nil).Collect()
	if err != nil {
		return ""
	}
	trashNames := []string{"Trash", "Deleted Items", "Deleted Messages", "[Gmail]/Trash"}
	for _, m := range mailboxes {
		for _, t := range trashNames {
			if strings.EqualFold(m.Mailbox, t) {
				return m.Mailbox
			}
		}
	}
	return ""
}

func hasAttachments(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	if sp, ok := bs.(*imap.BodyStructureSinglePart); ok {
		return sp.Filename() != ""
	}
	if mp, ok := bs.(*imap.BodyStructureMultiPart); ok {
		for _, child := range mp.Children {
			if hasAttachments(child) {
				return true
			}
		}
	}
	return false
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

var _ io.Closer = (*Client)(nil)
