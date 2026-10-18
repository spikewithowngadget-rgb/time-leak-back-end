package service

import (
	"context"
	"errors"
	"math"

	"time-leak/internal/domain"
)

var (
	ErrInvalidNoteDate   = errors.New("note_date must be a valid calendar date in YYYY-MM-DD format")
	ErrInvalidNoteFilter = errors.New("from and to must be valid YYYY-MM-DD dates with from <= to; page >= 1; limit 1..500")
)

func (s *AppService) ListNotesInRange(ctx context.Context, userID string, filter domain.NoteListFilter) ([]domain.Note, int, error) {
	if !domain.ValidNoteDate(filter.From) || !domain.ValidNoteDate(filter.To) || filter.From > filter.To ||
		filter.Page < 1 || filter.Limit < 1 || filter.Limit > 500 || (filter.Page-1) > math.MaxInt/filter.Limit {
		return nil, 0, ErrInvalidNoteFilter
	}
	return s.repo.ListNotesInRange(ctx, userID, filter)
}

func (s *AppService) ListNotesByDate(ctx context.Context, userID, date string) ([]domain.Note, error) {
	if !domain.ValidNoteDate(date) {
		return nil, ErrInvalidNoteFilter
	}
	notes, _, err := s.repo.ListNotesInRange(ctx, userID, domain.NoteListFilter{From: date, To: date})
	return notes, err
}
