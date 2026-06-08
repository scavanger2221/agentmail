package cache

import (
	"fmt"
	"os"
	"strings"

	"database/sql"
)

// EmailRow represents a row from the emails table.
type EmailRow struct {
	ID             int
	AccountID      int
	FolderID       int
	UID            int
	MessageID      string
	Subject        string
	FromAddr       string
	ToAddrs        string
	Date           string
	Flags          string
	BodySnippet    string
	BodyPath       string
	Size           int
	HasAttachments bool
	InternalDate   string
	IsSynced       bool
}

// InsertEmail inserts a new email metadata row.
func (s *Store) InsertEmail(accountID, folderID, uid int, messageID, subject, fromAddr, toAddrs, date, flags, bodySnippet string, size int, hasAttachments bool, internalDate string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO emails (account_id, folder_id, uid, message_id, subject, from_addr, to_addrs, date, flags, body_snippet, size, has_attachments, internal_date, is_synced)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`, accountID, folderID, uid, messageID, subject, fromAddr, toAddrs, date, flags, bodySnippet, size, boolToInt(hasAttachments), internalDate)
	return err
}

// UpdateEmailBody updates an email row with full body path and marks it synced.
func (s *Store) UpdateEmailBody(accountID, folderID, uid int, bodyPath, bodySnippet string, hasAttachments bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE emails SET body_path = ?, body_snippet = ?, has_attachments = ?, is_synced = 1, last_sync_at = datetime('now')
		WHERE account_id = ? AND folder_id = ? AND uid = ?
	`, bodyPath, bodySnippet, boolToInt(hasAttachments), accountID, folderID, uid)
	return err
}

// UpdateEmailBodySnippet updates just the body_snippet for an email (for search).
func (s *Store) UpdateEmailBodySnippet(accountID, uid int, bodySnippet string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snippet := firstN(bodySnippet, 1000)
	_, err := s.db.Exec(`
		UPDATE emails SET body_snippet = ?, is_synced = 1, last_sync_at = datetime('now')
		WHERE account_id = ? AND uid = ?
	`, snippet, accountID, uid)
	return err
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ListEmails returns a paginated list of emails in a folder.
func (s *Store) ListEmails(accountID, folderID int, limit, offset int) ([]EmailRow, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.Query(`
		SELECT id, account_id, folder_id, uid, COALESCE(message_id,''), COALESCE(subject,''), COALESCE(from_addr,''), COALESCE(to_addrs,''),
		       COALESCE(date,''), COALESCE(flags,''), COALESCE(body_snippet,''), COALESCE(body_path,''), size, has_attachments, COALESCE(internal_date,''), is_synced
		FROM emails
		WHERE account_id = ? AND folder_id = ?
		ORDER BY internal_date DESC
		LIMIT ? OFFSET ?
	`, accountID, folderID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEmails(rows)
}

// GetEmailByUID returns a single email by UID.
func (s *Store) GetEmailByUID(accountID, folderID, uid int) (*EmailRow, error) {
	var e EmailRow
	var hasAtt int
	var isSynced int
	err := s.db.QueryRow(`
		SELECT id, account_id, folder_id, uid, COALESCE(message_id,''), COALESCE(subject,''), COALESCE(from_addr,''), COALESCE(to_addrs,''),
		       COALESCE(date,''), COALESCE(flags,''), COALESCE(body_snippet,''), COALESCE(body_path,''), size, has_attachments, COALESCE(internal_date,''), is_synced
		FROM emails
		WHERE account_id = ? AND folder_id = ? AND uid = ?
	`, accountID, folderID, uid).Scan(
		&e.ID, &e.AccountID, &e.FolderID, &e.UID, &e.MessageID, &e.Subject, &e.FromAddr, &e.ToAddrs,
		&e.Date, &e.Flags, &e.BodySnippet, &e.BodyPath, &e.Size, &hasAtt, &e.InternalDate, &isSynced,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.HasAttachments = hasAtt != 0
	e.IsSynced = isSynced != 0
	return &e, nil
}

// SearchEmails performs a full-text search using FTS5.
func (s *Store) SearchEmails(accountID int, query string, limit int) ([]EmailRow, error) {
	if limit <= 0 {
		limit = 50
	}

	// FTS5: prepare query for substring matching
	ftsQuery := strings.Join(strings.Fields(query), " OR ")

	rows, err := s.db.Query(`
		SELECT e.id, e.account_id, e.folder_id, e.uid,
		       COALESCE(e.message_id,''), COALESCE(e.subject,''), COALESCE(e.from_addr,''), COALESCE(e.to_addrs,''),
		       COALESCE(e.date,''), COALESCE(e.flags,''), COALESCE(e.body_snippet,''), COALESCE(e.body_path,''),
		       e.size, e.has_attachments, COALESCE(e.internal_date,''), e.is_synced
		FROM emails e
		JOIN emails_fts fts ON e.id = fts.rowid
		WHERE e.account_id = ? AND emails_fts MATCH ?
		ORDER BY rank
		LIMIT ?
	`, accountID, ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEmails(rows)
}

// SearchEmailsSimple is a LIKE fallback when FTS5 isn't available.
func (s *Store) SearchEmailsSimple(accountID int, query string, limit int) ([]EmailRow, error) {
	if limit <= 0 {
		limit = 50
	}

	likeQuery := "%" + strings.ToLower(query) + "%"
	rows, err := s.db.Query(`
		SELECT id, account_id, folder_id, uid, COALESCE(message_id,''), COALESCE(subject,''), COALESCE(from_addr,''), COALESCE(to_addrs,''),
		       COALESCE(date,''), COALESCE(flags,''), COALESCE(body_snippet,''), COALESCE(body_path,''), size, has_attachments, COALESCE(internal_date,''), is_synced
		FROM emails
		WHERE account_id = ?
		  AND (LOWER(subject) LIKE ? OR LOWER(body_snippet) LIKE ? OR LOWER(from_addr) LIKE ?)
		ORDER BY internal_date DESC
		LIMIT ?
	`, accountID, likeQuery, likeQuery, likeQuery, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEmails(rows)
}

// DeleteEmailByUID deletes an email from the cache by UID.
func (s *Store) DeleteEmailByUID(accountID, folderID, uid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM emails WHERE account_id = ? AND folder_id = ? AND uid = ?`, accountID, folderID, uid)
	return err
}

// GetLastSyncUID returns the highest UID synced for a folder.
func (s *Store) GetLastSyncUID(folderID int) (int, error) {
	var uid int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(uid), 0) FROM emails WHERE folder_id = ?`, folderID).Scan(&uid)
	if err != nil {
		return 0, err
	}
	return uid, nil
}

// CountEmails returns the email count in the cache.
func (s *Store) CountEmails() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM emails`).Scan(&count)
	return count, err
}

// CountFolders returns the folder count.
func (s *Store) CountFolders() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM folders`).Scan(&count)
	return count, err
}

// CountAccounts returns the account count.
func (s *Store) CountAccounts() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM accounts`).Scan(&count)
	return count, err
}

// GetEmailsWithoutBodies returns UIDs of emails missing body text, ordered by most recent first.
func (s *Store) GetEmailsWithoutBodies(accountID, limit int) ([]int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT uid FROM emails
		WHERE account_id = ? AND (body_snippet IS NULL OR body_snippet = '' OR body_snippet = subject)
		ORDER BY internal_date DESC
		LIMIT ?
	`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var uids []int
	for rows.Next() {
		var uid int
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		uids = append(uids, uid)
	}
	return uids, rows.Err()
}

func scanEmails(rows *sql.Rows) ([]EmailRow, error) {
	var emails []EmailRow
	for rows.Next() {
		var e EmailRow
		var hasAtt int
		var isSynced int
		if err := rows.Scan(&e.ID, &e.AccountID, &e.FolderID, &e.UID, &e.MessageID, &e.Subject, &e.FromAddr, &e.ToAddrs,
			&e.Date, &e.Flags, &e.BodySnippet, &e.BodyPath, &e.Size, &hasAtt, &e.InternalDate, &isSynced); err != nil {
			return nil, err
		}
		e.HasAttachments = hasAtt != 0
		e.IsSynced = isSynced != 0
		emails = append(emails, e)
	}
	return emails, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DataPath returns the base directory for email bodies and attachments.
func DataPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return fmt.Sprintf("%s/.local/share/agentmail/data", home)
}
