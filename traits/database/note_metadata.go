package database

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"time-leak/internal/domain"
)

// Runs once, atomically with migration 0020. Do not infer selected dates from
// titles: old clients never supplied a trustworthy calendar date to the API.
func backfillNoteMetadata(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, note_files, note_date, note_date_source, strftime('%Y-%m-%d', created_at) FROM notes`)
	if err != nil {
		return err
	}
	type entry struct {
		id, date, source, fileType string
		count                      int
	}
	var updates []entry
	for rows.Next() {
		var id, raw, date, source string
		var fallback sql.NullString
		if err := rows.Scan(&id, &raw, &date, &source, &fallback); err != nil {
			rows.Close()
			return err
		}
		var files []string
		if err := json.Unmarshal([]byte(raw), &files); err != nil {
			files = nil // Match the existing reader's treatment of malformed JSON.
		}
		count, fileType := domain.NoteFileMetadata(files)
		if date == "" {
			date, source = fallback.String, domain.NoteDateCreatedAt
		}
		if !domain.ValidNoteDate(date) {
			rows.Close()
			return fmt.Errorf("cannot backfill calendar date for note %s", id)
		}
		updates = append(updates, entry{id, date, source, fileType, count})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := tx.Exec(`UPDATE notes SET note_date = ?, note_date_source = ?, file_count = ?, file_type = ? WHERE id = ?`,
			item.date, item.source, item.count, item.fileType, item.id); err != nil {
			return err
		}
	}
	return nil
}
