package repository

import (
	"context"
	"time"

	"time-leak/internal/domain"
)

func (r *Repository) CreateNoteShare(ctx context.Context, share domain.NoteShare) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO share_tokens (id, note_id, token, created_by, expires_at)
	 SELECT ?, id, ?, user_id, ? FROM notes WHERE id = ? AND user_id = ?`,
		share.ID, share.Token, share.ExpiresAt.UTC().Format(time.RFC3339), share.NoteID, share.CreatedBy)
	if err != nil {
		return err
	}
	return ensureRowsAffected(result)
}

func (r *Repository) GetNoteShare(ctx context.Context, token string) (domain.NoteShare, error) {
	var share domain.NoteShare
	var expiry, phone string
	err := r.db.QueryRowContext(ctx, `SELECT s.id, s.note_id, s.token, s.created_by, s.expires_at, s.views_count, COALESCE(u.phone, '')
	 FROM share_tokens s JOIN users u ON u.id = s.created_by WHERE s.token = ?`, token).
		Scan(&share.ID, &share.NoteID, &share.Token, &share.CreatedBy, &expiry, &share.ViewsCount, &phone)
	if err != nil {
		return share, err
	}
	share.ExpiresAt, err = time.Parse(time.RFC3339, expiry)
	if err != nil {
		return share, err
	}
	share.SharedBy = "Пользователь TimeLeak"
	if len(phone) >= 7 {
		share.SharedBy = phone[:2] + "•••••" + phone[len(phone)-4:]
	}
	return share, nil
}

func (r *Repository) CountNoteShareView(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE share_tokens SET views_count = views_count + 1 WHERE id = ?`, id)
	return err
}

func (r *Repository) CompletePastNotes(ctx context.Context, today string) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE notes SET is_completed = 1 WHERE lifecycle = 'one_time' AND is_completed = 0 AND note_date < ?`, today)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
