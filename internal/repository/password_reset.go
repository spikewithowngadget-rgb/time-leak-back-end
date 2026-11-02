package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"time-leak/internal/domain"
)

var ErrPasswordResetVerificationInvalid = errors.New("password reset verification is invalid")

// ResetPasswordWithVerification commits the new password and consumes its
// verification together, so a failed password update can safely be retried.
func (r *Repository) ResetPasswordWithVerification(ctx context.Context, verificationID, phone, passwordHash string, now time.Time) error {
	verificationID = strings.TrimSpace(verificationID)
	phone = normalizePhone(phone)
	passwordHash = strings.TrimSpace(passwordHash)
	if verificationID == "" || phone == "" {
		return ErrPasswordResetVerificationInvalid
	}
	if passwordHash == "" {
		return errors.New("password hash is empty")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer tx.Rollback()

	timestamp := now.UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
		UPDATE auth_verifications SET used_at = ?
		WHERE id = ? AND purpose = ? AND phone = ? AND used_at IS NULL
		  AND julianday(expires_at) >= julianday(?)`,
		timestamp, verificationID, domain.AuthVerificationPurposePasswordReset, phone, timestamp)
	if err != nil {
		return fmt.Errorf("consume password reset verification: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("count password reset verifications: %w", err)
	}
	if rows == 0 {
		return ErrPasswordResetVerificationInvalid
	}

	res, err = tx.ExecContext(ctx,
		`UPDATE users SET password = ?, updated_at = CURRENT_TIMESTAMP WHERE phone = ?`,
		passwordHash, phone)
	if err != nil {
		return fmt.Errorf("update password during reset: %w", err)
	}
	if err := ensureRowsAffected(res); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}
	return nil
}
