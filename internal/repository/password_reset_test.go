package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"time-leak/internal/domain"
	"time-leak/traits/database"

	"go.uber.org/zap"
)

type passwordResetFixture struct {
	repo  *Repository
	db    *sql.DB
	phone string
	now   time.Time
}

func newPasswordResetFixture(t *testing.T) passwordResetFixture {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "reset.db")+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := database.CreateTables(db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	f := passwordResetFixture{repo: New(db), db: db, phone: "+77015556677", now: time.Date(2026, 11, 2, 8, 45, 0, 0, time.UTC)}
	if _, err := f.repo.CreateUserWithPhone(context.Background(), "reset@example.test", f.phone, "old-hash", "en"); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f passwordResetFixture) verification(t *testing.T, purpose domain.AuthVerificationPurpose, expiresAt time.Time) domain.AuthVerification {
	t.Helper()
	ctx := context.Background()
	requestID := database.GenerateUUID()
	if _, err := f.repo.CreateOTPRequest(ctx, requestID, domain.OTPChannelWhatsApp, f.phone, purpose, "otp-hash", expiresAt, 5); err != nil {
		t.Fatal(err)
	}
	verification, err := f.repo.CreateAuthVerification(ctx, database.GenerateUUID(), requestID, purpose, f.phone, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return verification
}

func (f passwordResetFixture) requirePassword(t *testing.T, want string) {
	t.Helper()
	user, err := f.repo.GetUserByPhone(context.Background(), f.phone)
	if err != nil {
		t.Fatal(err)
	}
	if user.Password != want {
		t.Fatalf("password = %q, want %q", user.Password, want)
	}
}

func (f passwordResetFixture) requireUnused(t *testing.T, id string) {
	t.Helper()
	verification, err := f.repo.GetAuthVerificationByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if verification.UsedAt != nil {
		t.Fatal("failed password reset consumed verification")
	}
}

func TestResetPasswordWithVerificationSuccessAndSingleUse(t *testing.T) {
	f := newPasswordResetFixture(t)
	verification := f.verification(t, domain.AuthVerificationPurposePasswordReset, f.now.Add(5*time.Minute))
	ctx := context.Background()
	if err := f.repo.ResetPasswordWithVerification(ctx, " "+verification.ID+" ", "+7 (701) 555-66-77", "new-hash", f.now); err != nil {
		t.Fatal(err)
	}
	f.requirePassword(t, "new-hash")
	used, err := f.repo.GetAuthVerificationByID(ctx, verification.ID)
	if err != nil || used.UsedAt == nil || !used.UsedAt.Equal(f.now) {
		t.Fatalf("verification not consumed at reset time: %+v, err=%v", used, err)
	}
	if err := f.repo.ResetPasswordWithVerification(ctx, verification.ID, f.phone, "replayed-hash", f.now); !errors.Is(err, ErrPasswordResetVerificationInvalid) {
		t.Fatalf("replay error = %v, want invalid verification", err)
	}
	f.requirePassword(t, "new-hash")
}

func TestResetPasswordWithVerificationConcurrentSingleUse(t *testing.T) {
	f := newPasswordResetFixture(t)
	f.db.SetMaxOpenConns(4)
	verification := f.verification(t, domain.AuthVerificationPurposePasswordReset, f.now.Add(5*time.Minute))
	type result struct {
		hash string
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, hash := range []string{"first-hash", "second-hash"} {
		go func(hash string) {
			<-start
			results <- result{hash: hash, err: f.repo.ResetPasswordWithVerification(context.Background(), verification.ID, f.phone, hash, f.now)}
		}(hash)
	}
	close(start)
	var successfulHash string
	for range 2 {
		got := <-results
		if got.err == nil {
			if successfulHash != "" {
				t.Fatal("verification was consumed by two concurrent password resets")
			}
			successfulHash = got.hash
		} else if !errors.Is(got.err, ErrPasswordResetVerificationInvalid) {
			t.Fatalf("unexpected concurrent reset error: %v", got.err)
		}
	}
	if successfulHash == "" {
		t.Fatal("neither concurrent password reset succeeded")
	}
	f.requirePassword(t, successfulHash)
}

func TestResetPasswordWithVerificationRejectsInvalidVerification(t *testing.T) {
	for _, name := range []string{"registration", "wrong-phone", "expired", "missing"} {
		t.Run(name, func(t *testing.T) {
			f := newPasswordResetFixture(t)
			purpose := domain.AuthVerificationPurposePasswordReset
			if name == "registration" {
				purpose = domain.AuthVerificationPurposeRegistration
			}
			expiresAt := f.now.Add(5 * time.Minute)
			if name == "expired" {
				expiresAt = f.now.Add(-time.Second)
			}
			verification := f.verification(t, purpose, expiresAt)
			verificationID, phone := verification.ID, f.phone
			if name == "wrong-phone" {
				phone = "+77019998877"
			}
			if name == "missing" {
				verificationID = database.GenerateUUID()
			}
			err := f.repo.ResetPasswordWithVerification(context.Background(), verificationID, phone, "new-hash", f.now)
			if !errors.Is(err, ErrPasswordResetVerificationInvalid) {
				t.Fatalf("error = %v, want invalid verification", err)
			}
			f.requirePassword(t, "old-hash")
			f.requireUnused(t, verification.ID)
		})
	}
}

func TestResetPasswordWithVerificationRollsBackFailedPasswordUpdate(t *testing.T) {
	f := newPasswordResetFixture(t)
	verification := f.verification(t, domain.AuthVerificationPurposePasswordReset, f.now.Add(5*time.Minute))
	if _, err := f.db.Exec(`CREATE TRIGGER fail_password_reset BEFORE UPDATE OF password ON users BEGIN SELECT RAISE(ABORT, 'simulated write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	err := f.repo.ResetPasswordWithVerification(context.Background(), verification.ID, f.phone, "new-hash", f.now)
	if err == nil || errors.Is(err, ErrPasswordResetVerificationInvalid) {
		t.Fatalf("expected storage error, got %v", err)
	}
	f.requirePassword(t, "old-hash")
	f.requireUnused(t, verification.ID)
	if _, err := f.db.Exec(`DROP TRIGGER fail_password_reset`); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.ResetPasswordWithVerification(context.Background(), verification.ID, f.phone, "new-hash", f.now); err != nil {
		t.Fatalf("retry with original verification failed: %v", err)
	}
	f.requirePassword(t, "new-hash")
}

func TestResetPasswordWithVerificationRollsBackMissingUser(t *testing.T) {
	f := newPasswordResetFixture(t)
	verification := f.verification(t, domain.AuthVerificationPurposePasswordReset, f.now.Add(5*time.Minute))
	if _, err := f.db.Exec(`DELETE FROM users WHERE phone = ?`, f.phone); err != nil {
		t.Fatal(err)
	}
	err := f.repo.ResetPasswordWithVerification(context.Background(), verification.ID, f.phone, "new-hash", f.now)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
	f.requireUnused(t, verification.ID)
}
