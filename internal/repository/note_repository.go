package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"time-leak/internal/domain"
)

const noteMetadataColumns = `id, note_type, note_date, note_date_source, file_count, file_type, created_at, COALESCE(title, ''), lifecycle, recurrence_rule, is_completed`
const noteColumns = noteMetadataColumns + `, user_id, COALESCE(note_files, '[]'), title_audio_url`

type noteScanner interface {
	Scan(...any) error
}

func scanNote(row noteScanner, metadataOnly bool) (domain.Note, error) {
	var note domain.Note
	var createdAt, files string
	args := []any{&note.ID, &note.NoteType, &note.NoteDate, &note.NoteDateSource,
		&note.FileCount, &note.FileType, &createdAt, &note.Title, &note.Lifecycle, &note.RecurrenceRule, &note.IsCompleted}
	if !metadataOnly {
		args = append(args, &note.UserID, &files, &note.TitleAudioURL)
	}
	if err := row.Scan(args...); err != nil {
		return domain.Note{}, err
	}
	note.CreatedAt = parseSQLiteTime(createdAt)
	if note.Title == "" {
		note.Title = domain.DefaultNoteTitle(note.NoteType, note.NoteDate)
	}
	note.Occurrences = []string{}
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
	where := ` FROM notes WHERE user_id = ? AND ((note_date >= ? AND note_date <= ?) OR (lifecycle = 'recurring' AND note_date <= ?))`
	args := []any{userID, filter.From, filter.To, filter.To}
	// Match and count from metadata first; only the selected page reads file URLs.
	query := `SELECT ` + noteMetadataColumns + where + ` ORDER BY note_date ASC, created_at ASC, id ASC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	notes := make([]domain.Note, 0)
	total := 0
	for rows.Next() {
		note, err := scanNote(rows, true)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		if note.Lifecycle == domain.NoteRecurring && !domain.HasNoteOccurrence(note, filter.From, filter.To) {
			continue
		}
		total++
		if filter.Limit == 0 || (total > (filter.Page-1)*filter.Limit && len(notes) < filter.Limit) {
			if note.Lifecycle == domain.NoteRecurring {
				note.Occurrences = domain.NoteOccurrences(note, filter.From, filter.To)
			}
			notes = append(notes, note)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if !filter.MetadataOnly {
		for start := 0; start < len(notes); start += 500 {
			end := start + 500
			if end > len(notes) {
				end = len(notes)
			}
			args := []any{userID}
			positions := make(map[string]int, end-start)
			for i := start; i < end; i++ {
				args = append(args, notes[i].ID)
				positions[notes[i].ID] = i
			}
			query := `SELECT ` + noteColumns + ` FROM notes WHERE user_id = ? AND id IN (` + strings.TrimSuffix(strings.Repeat("?,", end-start), ",") + `)`
			pageRows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				return nil, 0, err
			}
			for pageRows.Next() {
				note, err := scanNote(pageRows, false)
				if err != nil {
					pageRows.Close()
					return nil, 0, err
				}
				index := positions[note.ID]
				note.Occurrences = notes[index].Occurrences
				notes[index] = note
			}
			if err := pageRows.Err(); err != nil {
				pageRows.Close()
				return nil, 0, err
			}
			if err := pageRows.Close(); err != nil {
				return nil, 0, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return notes, total, nil
}
