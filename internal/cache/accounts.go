package cache

import (
	"database/sql"
)

// AccountRow represents a row from the accounts table.
type AccountRow struct {
	ID           int
	Name         string
	Email        string
	IMAPHost     string
	IMAPPort     int
	SMTPHost     string
	SMTPPort     int
	AuthMethod   string
	PasswordFile string
}

// InsertAccount adds or replaces an account in the cache.
func (s *Store) InsertAccount(name, email, imapHost string, imapPort int, smtpHost string, smtpPort int, authMethod, passwordFile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO accounts (name, email, imap_host, imap_port, smtp_host, smtp_port, auth_method, password_file)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			email=excluded.email,
			imap_host=excluded.imap_host,
			imap_port=excluded.imap_port,
			smtp_host=excluded.smtp_host,
			smtp_port=excluded.smtp_port,
			auth_method=excluded.auth_method,
			password_file=excluded.password_file
	`, name, email, imapHost, imapPort, smtpHost, smtpPort, authMethod, passwordFile)
	return err
}

// RemoveAccount deletes an account and all its data (cascading).
func (s *Store) RemoveAccount(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM accounts WHERE name = ?`, name)
	return err
}

// ListAccounts returns all configured accounts.
func (s *Store) ListAccounts() ([]AccountRow, error) {
	rows, err := s.db.Query(`SELECT id, name, email, imap_host, imap_port, smtp_host, smtp_port, auth_method, COALESCE(password_file,'') FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []AccountRow
	for rows.Next() {
		var a AccountRow
		if err := rows.Scan(&a.ID, &a.Name, &a.Email, &a.IMAPHost, &a.IMAPPort, &a.SMTPHost, &a.SMTPPort, &a.AuthMethod, &a.PasswordFile); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// GetAccountByID returns an account by its ID.
func (s *Store) GetAccountByID(id int) (*AccountRow, error) {
	var a AccountRow
	err := s.db.QueryRow(`SELECT id, name, email, imap_host, imap_port, smtp_host, smtp_port, auth_method, COALESCE(password_file,'') FROM accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Email, &a.IMAPHost, &a.IMAPPort, &a.SMTPHost, &a.SMTPPort, &a.AuthMethod, &a.PasswordFile)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAccountByName returns an account by its name.
func (s *Store) GetAccountByName(name string) (*AccountRow, error) {
	var a AccountRow
	err := s.db.QueryRow(`SELECT id, name, email, imap_host, imap_port, smtp_host, smtp_port, auth_method, COALESCE(password_file,'') FROM accounts WHERE name = ?`, name).
		Scan(&a.ID, &a.Name, &a.Email, &a.IMAPHost, &a.IMAPPort, &a.SMTPHost, &a.SMTPPort, &a.AuthMethod, &a.PasswordFile)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
