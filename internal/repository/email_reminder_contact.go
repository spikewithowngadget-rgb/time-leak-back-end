package repository

import (
	"context"
	"time"

	"time-leak/internal/domain"
)

func (r *Repository) GetEmailReminderContact(ctx context.Context, userID string) (domain.EmailReminderContact, error) {
	var contact domain.EmailReminderContact
	var verifiedAt, updatedAt string
	err := r.db.QueryRowContext(ctx, `SELECT user_id,email,verified_at,enabled,language,updated_at FROM user_email_reminder_contacts WHERE user_id=?`, userID).
		Scan(&contact.UserID, &contact.Email, &verifiedAt, &contact.Enabled, &contact.Language, &updatedAt)
	if err != nil {
		return domain.EmailReminderContact{}, err
	}
	verified := parseSQLiteTime(verifiedAt)
	contact.VerifiedAt = &verified
	contact.UpdatedAt = parseSQLiteTime(updatedAt)
	return contact, nil
}

func (r *Repository) UpsertVerifiedEmailReminderContact(ctx context.Context, userID, email string, enabled bool, language string) (domain.EmailReminderContact, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := r.db.ExecContext(ctx, `INSERT INTO user_email_reminder_contacts(user_id,email,verified_at,enabled,language,updated_at)
		SELECT id,?,?,?,?,? FROM users WHERE id=? AND COALESCE(is_active,1)=1
		ON CONFLICT(user_id) DO UPDATE SET email=excluded.email,verified_at=excluded.verified_at,enabled=excluded.enabled,language=excluded.language,updated_at=excluded.updated_at`, email, now, boolToInt(enabled), language, now, userID)
	if err != nil {
		return domain.EmailReminderContact{}, err
	}
	if err := ensureRowsAffected(res); err != nil {
		return domain.EmailReminderContact{}, err
	}
	return r.GetEmailReminderContact(ctx, userID)
}

func (r *Repository) SetEmailReminderContactEnabled(ctx context.Context, userID string, enabled bool, language *string) (domain.EmailReminderContact, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE user_email_reminder_contacts SET enabled=?,updated_at=?,language=COALESCE(?,language) WHERE user_id=? AND verified_at<>''
		AND EXISTS(SELECT 1 FROM users WHERE id=? AND COALESCE(is_active,1)=1)`, boolToInt(enabled), time.Now().UTC().Format(time.RFC3339Nano), language, userID, userID)
	if err != nil {
		return domain.EmailReminderContact{}, err
	}
	if err := ensureRowsAffected(res); err != nil {
		return domain.EmailReminderContact{}, err
	}
	return r.GetEmailReminderContact(ctx, userID)
}
