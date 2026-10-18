package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"time-leak/internal/domain"
)

const noteMetadataColumns = `id, note_type, note_date, note_date_source, file_count, file_type, created_at`
const noteColumns = noteMetadataColumns + `, user_id, COALESCE(note_files, '[]')`

type noteScanner interface {
	Scan(...any) error
}

func scanNote(row noteScanner, metadataOnly bool) (domain.Note, error) {
	var note domain.Note
	var createdAt, files string
	args := []any{&note.ID, &note.NoteType, &note.NoteDate, &note.NoteDateSource,
		&note.FileCount, &note.FileType, &createdAt}
	if !metadataOnly {
		args = append(args, &note.UserID, &files)
	}
	if err := row.Scan(args...); err != nil {
		return domain.Note{}, err
	}
	note.CreatedAt = parseSQLiteTime(createdAt)
	if !metadataOnly {
		note.NoteFiles = unmarshalNoteFiles(files)
	}
	return note, nil
}

// ListNotesInRange counts and reads within one snapshot, with an explicit SQL
// projection for metadata. A zero limit is used only for the unpaginated day API.
func (r *Repository) ListNotesInRange(ctx context.Context, userID string, filter domain.NoteListFilter) ([]domain.Note, int, error) {
	if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
		return nil, 0, errors.New("userId must be valid UUID")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	where := ` FROM notes WHERE user_id = ? AND note_date >= ? AND note_date <= ?`
	args := []any{userID, filter.From, filter.To}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	columns := noteColumns
	if filter.MetadataOnly {
		columns = noteMetadataColumns
	}
	query := `SELECT ` + columns + where + ` ORDER BY note_date ASC, created_at ASC, id ASC`
	if filter.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, filter.Limit, (filter.Page-1)*filter.Limit)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	notes := make([]domain.Note, 0)
	for rows.Next() {
		note, err := scanNote(rows, filter.MetadataOnly)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return notes, total, nil
}
