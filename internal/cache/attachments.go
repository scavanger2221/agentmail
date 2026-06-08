package cache

// AttachmentRow represents a row from the attachments table.
type AttachmentRow struct {
	ID          int
	EmailID     int
	Filename    string
	MimeType    string
	Size        int
	StoragePath string
}

// InsertAttachment inserts a new attachment row.
func (s *Store) InsertAttachment(emailID int, filename, mimeType string, size int, storagePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO attachments (email_id, filename, mime_type, size, storage_path)
		VALUES (?, ?, ?, ?, ?)
	`, emailID, filename, mimeType, size, storagePath)
	return err
}

// ListAttachments returns all attachments for an email.
func (s *Store) ListAttachments(emailID int) ([]AttachmentRow, error) {
	rows, err := s.db.Query(`SELECT id, email_id, filename, COALESCE(mime_type,''), size, COALESCE(storage_path,'') FROM attachments WHERE email_id = ?`, emailID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var atts []AttachmentRow
	for rows.Next() {
		var a AttachmentRow
		if err := rows.Scan(&a.ID, &a.EmailID, &a.Filename, &a.MimeType, &a.Size, &a.StoragePath); err != nil {
			return nil, err
		}
		atts = append(atts, a)
	}
	return atts, rows.Err()
}
