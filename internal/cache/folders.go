package cache

import "database/sql"

// FolderRow represents a row from the folders table.
type FolderRow struct {
	ID           int
	AccountID    int
	Name         string
	UIDValidity  int
	LastSyncUID  int
}

// UpsertFolder inserts or updates a folder.
func (s *Store) UpsertFolder(accountID int, name string, uidValidity int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO folders (account_id, name, uid_validity, last_sync_uid)
		VALUES (?, ?, ?, 0)
		ON CONFLICT(account_id, name) DO UPDATE SET uid_validity=excluded.uid_validity
	`, accountID, name, uidValidity)
	if err != nil {
		return 0, err
	}

	// Get the ID (either inserted or existing)
	var id int
	err = s.db.QueryRow(`SELECT id FROM folders WHERE account_id = ? AND name = ?`, accountID, name).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListFolders returns all folders for an account.
func (s *Store) ListFolders(accountID int) ([]FolderRow, error) {
	rows, err := s.db.Query(`SELECT id, account_id, name, uid_validity, last_sync_uid FROM folders WHERE account_id = ? ORDER BY name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []FolderRow
	for rows.Next() {
		var f FolderRow
		if err := rows.Scan(&f.ID, &f.AccountID, &f.Name, &f.UIDValidity, &f.LastSyncUID); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

// GetFolder returns a folder by account and name.
func (s *Store) GetFolder(accountID int, name string) (*FolderRow, error) {
	var f FolderRow
	err := s.db.QueryRow(`SELECT id, account_id, name, uid_validity, last_sync_uid FROM folders WHERE account_id = ? AND name = ?`, accountID, name).
		Scan(&f.ID, &f.AccountID, &f.Name, &f.UIDValidity, &f.LastSyncUID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// UpdateFolderSync updates the last_sync_uid and uid_validity for a folder.
func (s *Store) UpdateFolderSync(folderID int, uidValidity, lastUID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE folders SET uid_validity = ?, last_sync_uid = ? WHERE id = ?`, uidValidity, lastUID, folderID)
	return err
}
