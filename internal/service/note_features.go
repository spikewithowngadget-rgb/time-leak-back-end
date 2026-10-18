package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"time-leak/internal/domain"
)

var (
	ErrShareExpired       = errors.New("share link expired")
	ErrInvalidShareExpiry = errors.New("expires_in must be between 1 and 8760 hours")
)

func (s *AppService) CreateNoteShare(ctx context.Context, noteID, userID string, hours int) (domain.NoteShare, error) {
	if hours < 1 || hours > 8760 {
		return domain.NoteShare{}, ErrInvalidShareExpiry
	}
	if _, err := s.repo.GetNoteByIDForUser(ctx, noteID, userID); err != nil {
		return domain.NoteShare{}, err
	}
	var random [9]byte
	if _, err := rand.Read(random[:]); err != nil {
		return domain.NoteShare{}, err
	}
	share := domain.NoteShare{ID: uuid.NewString(), NoteID: noteID, CreatedBy: userID,
		Token: base64.RawURLEncoding.EncodeToString(random[:]), ExpiresAt: time.Now().UTC().Add(time.Duration(hours) * time.Hour)}
	return share, s.repo.CreateNoteShare(ctx, share)
}

func (s *AppService) GetSharedNote(ctx context.Context, token string, countView bool) (domain.Note, domain.NoteShare, error) {
	if len(token) != 12 || strings.ContainsAny(token, "/\\. ") {
		return domain.Note{}, domain.NoteShare{}, ErrInvalidSharedToken
	}
	share, err := s.repo.GetNoteShare(ctx, token)
	if err != nil {
		return domain.Note{}, share, err
	}
	if !time.Now().UTC().Before(share.ExpiresAt) {
		return domain.Note{}, share, ErrShareExpired
	}
	note, err := s.repo.GetNoteByIDForUser(ctx, share.NoteID, share.CreatedBy)
	if err == nil && countView {
		err = s.repo.CountNoteShareView(ctx, share.ID)
	}
	return note, share, err
}

var ErrInvalidSharedToken = errors.New("share link not found")

// RunNoteCompletion catches up after a restart, then runs daily at 00:05 UTC.
// Civil note dates are deliberately not interpreted in the server's local zone.
func (s *AppService) RunNoteCompletion(ctx context.Context) {
	for {
		now := time.Now().UTC()
		if _, err := s.repo.CompletePastNotes(ctx, now.Format(time.DateOnly)); err != nil && ctx.Err() == nil {
			s.log.Error("complete past notes", zap.Error(err))
		}
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
