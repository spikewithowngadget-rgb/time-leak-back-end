package repository

import (
	"context"
	"database/sql"
	"math"
	"time"

	"time-leak/internal/domain"
)

// Every email quota timestamp has the same UTC precision for indexed comparison.
const emailOTPTimeFormat = "2006-01-02T15:04:05.000000000Z"

func (r *Repository) CreateEmailOTPRequest(ctx context.Context, requestID, email string,
	purpose domain.AuthVerificationPurpose, codeHash string, expiresAt time.Time,
	maxAttempts int, cooldown time.Duration, dailyLimit int) (domain.OTPRequest, error) {
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.OTPRequest{}, err
	}
	defer tx.Rollback()
	// One guarded write reserves the quota under SQLite's writer lock. Parallel
	// requests, including those from other application processes, cannot overbook.
	res, err := tx.ExecContext(ctx, `INSERT INTO email_otp_send_attempts(request_id,email,requested_at)
		SELECT ?, ?, ? WHERE
		NOT EXISTS (SELECT 1 FROM email_otp_send_attempts WHERE email=? AND requested_at>?)
		AND (SELECT COUNT(*) FROM email_otp_send_attempts WHERE email=? AND requested_at>?) < ?`,
		requestID, email, now.Format(emailOTPTimeFormat),
		email, now.Add(-cooldown).Format(emailOTPTimeFormat),
		email, now.Add(-24*time.Hour).Format(emailOTPTimeFormat), dailyLimit)
	if err != nil {
		return domain.OTPRequest{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return domain.OTPRequest{}, err
	}
	if affected == 0 {
		var count int
		var oldest, latest sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),MIN(requested_at) FROM email_otp_send_attempts WHERE email=? AND requested_at>?`,
			email, now.Add(-24*time.Hour).Format(emailOTPTimeFormat)).Scan(&count, &oldest); err != nil {
			return domain.OTPRequest{}, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT MAX(requested_at) FROM email_otp_send_attempts WHERE email=?`, email).Scan(&latest); err != nil {
			return domain.OTPRequest{}, err
		}
		retryAt := parseSQLiteTime(latest.String).Add(cooldown)
		if count >= dailyLimit {
			retryAt = parseSQLiteTime(oldest.String).Add(24 * time.Hour)
		}
		seconds := int(math.Ceil(retryAt.Sub(now).Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		return domain.OTPRequest{}, &domain.EmailOTPRequestLimitError{RetryAfterSeconds: seconds, Daily: count >= dailyLimit}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO otp_requests(id,channel,destination,purpose,code_hash,expires_at,max_attempts,created_at)
		VALUES (?,'email',?,?,?,?,?,?)`, requestID, email, string(purpose), codeHash, expiresAt.UTC().Format(time.RFC3339Nano), maxAttempts, now.Format(emailOTPTimeFormat))
	if err != nil {
		return domain.OTPRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.OTPRequest{}, err
	}
	return r.GetOTPRequestByID(ctx, requestID)
}

func (r *Repository) IncrementEmailOTPAttempt(ctx context.Context, requestID string) (int, error) {
	var attempts int
	err := r.db.QueryRowContext(ctx, `UPDATE otp_requests SET attempts=attempts+1,last_attempt_at=?
		WHERE id=? AND channel='email' AND used_at IS NULL AND attempts<max_attempts
		AND julianday(expires_at)>julianday(?) RETURNING attempts`,
		time.Now().UTC().Format(time.RFC3339Nano), requestID, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&attempts)
	return attempts, err
}

func (r *Repository) MarkEmailOTPUsed(ctx context.Context, requestID string, usedAt time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE otp_requests SET used_at=?
		WHERE id=? AND channel='email' AND used_at IS NULL AND attempts<max_attempts
		AND julianday(expires_at)>julianday(?)
		AND id=(SELECT id FROM otp_requests candidate WHERE candidate.channel='email'
		 AND candidate.destination=otp_requests.destination ORDER BY created_at DESC,id DESC LIMIT 1)`,
		usedAt.UTC().Format(time.RFC3339Nano), requestID, usedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return ensureRowsAffected(res)
}
