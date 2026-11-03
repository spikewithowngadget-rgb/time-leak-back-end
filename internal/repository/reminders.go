package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"time-leak/internal/domain"
)

const reminderSettingsColumns = `enabled, days_before, send_hour, timezone, revision, updated_at`

func scanReminderSettings(row noteScanner) (domain.ReminderSettings, error) {
	var settings domain.ReminderSettings
	var updated string
	if err := row.Scan(&settings.Enabled, &settings.DaysBefore, &settings.SendHour, &settings.Timezone, &settings.Revision, &updated); err != nil {
		return settings, err
	}
	settings.UpdatedAt = parseSQLiteTime(updated)
	return settings, nil
}

func (r *Repository) GetReminderSettings(ctx context.Context) (domain.ReminderSettings, error) {
	return scanReminderSettings(r.db.QueryRowContext(ctx, `SELECT `+reminderSettingsColumns+` FROM email_reminder_settings WHERE id = 1`))
}

func (r *Repository) UpdateReminderSettings(ctx context.Context, settings domain.ReminderSettings) (domain.ReminderSettings, error) {
	if err := domain.ValidateReminderSettings(settings); err != nil {
		return domain.ReminderSettings{}, err
	}
	return scanReminderSettings(r.db.QueryRowContext(ctx, `UPDATE email_reminder_settings
		SET enabled = ?, days_before = ?, send_hour = ?, timezone = ?, revision = revision + 1, updated_at = ?
		WHERE id = 1 RETURNING `+reminderSettingsColumns,
		settings.Enabled, settings.DaysBefore, settings.SendHour, settings.Timezone, time.Now().UTC().Format(time.RFC3339Nano)))
}

const reminderCandidateColumns = `n.id, n.user_id, n.note_type, n.note_date, n.note_date_source,
	n.file_count, n.file_type, n.created_at, COALESCE(n.title, ''), n.lifecycle, n.recurrence_rule,
	n.is_completed, c.email, COALESCE(NULLIF(c.language, ''), u.user_language, 'en'), c.updated_at`
const reminderCandidateJoins = ` FROM notes n
	JOIN users u ON u.id = n.user_id
	JOIN user_email_reminder_contacts c ON c.user_id = u.id
	WHERE COALESCE(u.is_active, 1) = 1 AND c.enabled = 1 AND c.verified_at <> ''
	AND (n.lifecycle = 'recurring' OR n.is_completed = 0)`

func scanReminderCandidate(row noteScanner) (domain.ReminderCandidate, error) {
	var candidate domain.ReminderCandidate
	var created, contactUpdated string
	note := &candidate.Note
	err := row.Scan(&note.ID, &note.UserID, &note.NoteType, &note.NoteDate, &note.NoteDateSource,
		&note.FileCount, &note.FileType, &created, &note.Title, &note.Lifecycle, &note.RecurrenceRule,
		&note.IsCompleted, &candidate.Email, &candidate.Language, &contactUpdated)
	if err != nil {
		return candidate, err
	}
	note.CreatedAt = parseSQLiteTime(created)
	candidate.ContactUpdatedAt = parseSQLiteTime(contactUpdated)
	return candidate, nil
}

// Candidates use note metadata only. File paths, private media and passwords
// never enter the reminder worker or the provider payload.
func (r *Repository) ListReminderCandidates(ctx context.Context, from, to string) ([]domain.ReminderCandidate, error) {
	if !domain.ValidNoteDate(from) || !domain.ValidNoteDate(to) || from > to {
		return nil, errors.New("invalid reminder date range")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+reminderCandidateColumns+reminderCandidateJoins+`
		AND ((n.note_date BETWEEN ? AND ?) OR (n.lifecycle = 'recurring' AND n.note_date <= ?))
		ORDER BY n.user_id, n.note_date, n.id`, from, to, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.ReminderCandidate, 0)
	for rows.Next() {
		candidate, err := scanReminderCandidate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

// Fetching again immediately before delivery makes opt-out, mailbox changes,
// note deletion/completion and account deactivation effective for queued jobs.
func (r *Repository) GetReminderCandidate(ctx context.Context, userID, noteID string) (domain.ReminderCandidate, error) {
	return scanReminderCandidate(r.db.QueryRowContext(ctx, `SELECT `+reminderCandidateColumns+reminderCandidateJoins+`
		AND n.user_id = ? AND n.id = ?`, userID, noteID))
}

func (r *Repository) EnqueueReminderDelivery(ctx context.Context, delivery domain.ReminderDelivery, now time.Time) (bool, error) {
	if delivery.ID == "" {
		delivery.ID = uuid.NewString()
	}
	if !domain.ValidNoteDate(delivery.OccurrenceDate) || !domain.ValidNoteDate(delivery.NotificationDate) || delivery.DaysBefore < 1 || delivery.DaysBefore > 30 {
		return false, errors.New("invalid reminder occurrence")
	}
	occurrence, _ := time.Parse(time.DateOnly, delivery.OccurrenceDate)
	if occurrence.AddDate(0, 0, -delivery.DaysBefore).Format(time.DateOnly) != delivery.NotificationDate {
		return false, errors.New("reminder date does not match occurrence")
	}
	email, valid := domain.NormalizeEmailAddress(delivery.Email)
	if !valid || email != delivery.Email || strings.HasSuffix(email, "@otp.local") || delivery.Subject == "" || delivery.Text == "" || delivery.HTML == "" {
		return false, errors.New("invalid reminder payload")
	}
	next := now
	if !delivery.NextAttemptAt.IsZero() {
		next = delivery.NextAttemptAt
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO email_reminder_deliveries
		(id, user_id, note_id, occurrence_date, days_before, notification_date, email, language, title,
		 subject, text_body, html_body, status, next_attempt_at, created_at, updated_at)
		SELECT ?, n.user_id, n.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?
		FROM notes n JOIN users u ON u.id = n.user_id
		JOIN user_email_reminder_contacts c ON c.user_id = u.id
		WHERE n.id = ? AND n.user_id = ? AND COALESCE(u.is_active, 1) = 1
		AND c.enabled = 1 AND c.verified_at <> '' AND c.email = ?
		AND EXISTS (SELECT 1 FROM email_reminder_settings WHERE id = 1 AND enabled = 1)
		ON CONFLICT(user_id, note_id, occurrence_date, days_before) DO NOTHING`,
		delivery.ID, delivery.OccurrenceDate, delivery.DaysBefore, delivery.NotificationDate,
		delivery.Email, delivery.Language, delivery.Title, delivery.Subject, delivery.Text, delivery.HTML,
		next.UnixMilli(), now.UnixMilli(), now.UnixMilli(), delivery.NoteID, delivery.UserID, delivery.Email)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

const reminderDeliveryColumns = `id, user_id, note_id, occurrence_date, days_before, notification_date,
	email, language, title, subject, text_body, html_body, status, attempts, lease_token, lease_until,
	next_attempt_at, created_at, updated_at, sent_at, provider_id, last_error`

func scanReminderDelivery(row noteScanner) (domain.ReminderDelivery, error) {
	var delivery domain.ReminderDelivery
	var lease, next, created, updated int64
	var sent sql.NullInt64
	err := row.Scan(&delivery.ID, &delivery.UserID, &delivery.NoteID, &delivery.OccurrenceDate,
		&delivery.DaysBefore, &delivery.NotificationDate, &delivery.Email, &delivery.Language,
		&delivery.Title, &delivery.Subject, &delivery.Text, &delivery.HTML, &delivery.Status,
		&delivery.Attempts, &delivery.LeaseToken, &lease, &next, &created, &updated, &sent,
		&delivery.ProviderID, &delivery.LastError)
	if err != nil {
		return delivery, err
	}
	if lease != 0 {
		delivery.LeaseUntil = time.UnixMilli(lease).UTC()
	}
	delivery.NextAttemptAt = time.UnixMilli(next).UTC()
	delivery.CreatedAt = time.UnixMilli(created).UTC()
	delivery.UpdatedAt = time.UnixMilli(updated).UTC()
	if sent.Valid {
		timestamp := time.UnixMilli(sent.Int64).UTC()
		delivery.SentAt = &timestamp
	}
	return delivery, nil
}

// One SQL UPDATE claims one job, including recovery of an expired worker lease.
// Both claim and acknowledgement use random fencing tokens. A stale worker
// cannot overwrite the result after a new worker has acquired the job.
func (r *Repository) ClaimReminderDelivery(ctx context.Context, today string, now, leaseUntil time.Time) (domain.ReminderDelivery, error) {
	if !domain.ValidNoteDate(today) || !leaseUntil.After(now) {
		return domain.ReminderDelivery{}, errors.New("invalid reminder claim window")
	}
	// Resend retains idempotency keys for 24 hours. Even a timezone/DST change
	// must not permit an ambiguous retry beyond that provider guarantee.
	oldest := now.Add(-23 * time.Hour).UnixMilli()
	if _, err := r.db.ExecContext(ctx, `UPDATE email_reminder_deliveries
		SET status = 'failed', last_error = 'retry_window_exhausted', lease_token = '', lease_until = 0, updated_at = ?
		WHERE notification_date = ? AND (attempts >= ? OR created_at < ?)
		AND (status IN ('pending', 'retry') OR (status = 'sending' AND lease_until <= ?))`,
		now.UnixMilli(), today, domain.MaxReminderAttempts, oldest, now.UnixMilli()); err != nil {
		return domain.ReminderDelivery{}, err
	}
	return scanReminderDelivery(r.db.QueryRowContext(ctx, `UPDATE email_reminder_deliveries
		SET status = 'sending', lease_token = ?, lease_until = ?, attempts = attempts + 1, updated_at = ?
		WHERE id = (SELECT id FROM email_reminder_deliveries
			WHERE notification_date = ? AND next_attempt_at <= ? AND attempts < ? AND created_at >= ?
			AND (status IN ('pending', 'retry') OR (status = 'sending' AND lease_until <= ?))
			AND EXISTS (SELECT 1 FROM email_reminder_settings WHERE id = 1 AND enabled = 1)
			ORDER BY next_attempt_at, created_at, id LIMIT 1)
		RETURNING `+reminderDeliveryColumns, uuid.NewString(), leaseUntil.UnixMilli(), now.UnixMilli(),
		today, now.UnixMilli(), domain.MaxReminderAttempts, oldest, now.UnixMilli()))
}

func (r *Repository) FinishReminderDelivery(ctx context.Context, id, leaseToken, status, providerID, lastError string, nextAttemptAt, now time.Time) error {
	if id == "" || leaseToken == "" {
		return errors.New("missing reminder claim")
	}
	switch status {
	case domain.ReminderSent, domain.ReminderRetry, domain.ReminderFailed, domain.ReminderCancelled:
	default:
		return errors.New("invalid reminder delivery outcome")
	}
	var sent any
	if status == domain.ReminderSent {
		sent = now.UnixMilli()
	}
	if len(lastError) > 512 {
		lastError = lastError[:512]
	}
	result, err := r.db.ExecContext(ctx, `UPDATE email_reminder_deliveries
		SET status = ?, provider_id = ?, last_error = ?, next_attempt_at = ?, sent_at = ?,
		lease_token = '', lease_until = 0, updated_at = ?
		WHERE id = ? AND status = 'sending' AND lease_token = ?`,
		status, providerID, lastError, nextAttemptAt.UnixMilli(), sent, now.UnixMilli(), id, leaseToken)
	if err != nil {
		return err
	}
	return ensureRowsAffected(result)
}

func (r *Repository) CancelReminderDeliveriesBefore(ctx context.Context, today string, now time.Time) error {
	if !domain.ValidNoteDate(today) {
		return errors.New("invalid reminder civil date")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE email_reminder_deliveries
		SET status = 'cancelled', last_error = 'past_reminder_date', lease_token = '', lease_until = 0, updated_at = ?
		WHERE notification_date < ? AND (status IN ('pending', 'retry') OR (status = 'sending' AND lease_until <= ?))`,
		now.UnixMilli(), today, now.UnixMilli())
	return err
}

func (r *Repository) GetReminderStatus(ctx context.Context) (domain.ReminderStatus, error) {
	var status domain.ReminderStatus
	var last sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN status IN ('pending', 'retry', 'sending') THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END), 0), MAX(sent_at)
		FROM email_reminder_deliveries`).Scan(&status.Pending, &status.Sent, &status.Failed, &status.Cancelled, &last)
	if err != nil {
		return status, err
	}
	if last.Valid {
		timestamp := time.UnixMilli(last.Int64).UTC()
		status.LastSentAt = &timestamp
	}
	err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_email_reminder_contacts c
		JOIN users u ON u.id = c.user_id WHERE c.enabled = 1 AND c.verified_at <> '' AND COALESCE(u.is_active, 1) = 1`).Scan(&status.VerifiedContacts)
	return status, err
}
