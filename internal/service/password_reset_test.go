package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/repository"
	"time-leak/traits/database"

	"go.uber.org/zap"
)

func newPasswordResetService(t *testing.T) (*AppService, *sql.DB, string, string) {
	t.Helper()
	db, err := database.InitDatabase(&config.Config{
		DBPath: t.TempDir(), DBName: "reset.db", MaxOpenConns: 4, MaxIdleConns: 4,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := repository.New(db)
	svc := NewAppService(repo, 5*time.Minute, zap.NewNop())
	ctx := context.Background()
	phone := "+77015556677"
	hash, err := hashPassword("OriginalPass123!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateUserWithPhone(ctx, "reset@example.test", phone, hash, "en"); err != nil {
		t.Fatal(err)
	}
	requestID := database.GenerateUUID()
	if _, err := repo.CreateOTPRequest(ctx, requestID, domain.OTPChannelWhatsApp, phone,
		domain.AuthVerificationPurposePasswordReset, "otp-hash", time.Now().Add(5*time.Minute), 5); err != nil {
		t.Fatal(err)
	}
	verification, err := svc.CreateAuthVerification(ctx, domain.AuthVerificationPurposePasswordReset, requestID, phone)
	if err != nil {
		t.Fatal(err)
	}
	return svc, db, phone, verification.VerificationToken
}

func TestResetPasswordValidationAllowsRetryWithSameToken(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
		confirm  string
		want     error
	}{
		{"missing", "", "", ErrPasswordRequired},
		{"short", "Ab1!cd", "Ab1!cd", ErrPasswordTooShort},
		{"mismatch", "NextPass123!", "OtherPass123!", ErrPasswordMismatch},
		{"over-bcrypt-limit", strings.Repeat("a", 73), strings.Repeat("a", 73), ErrPasswordTooLong},
		{"multibyte-over-bcrypt-limit", strings.Repeat("я", 37), strings.Repeat("я", 37), ErrPasswordTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, phone, token := newPasswordResetService(t)
			ctx := context.Background()
			if err := svc.ResetPasswordWithOTP(ctx, phone, tc.password, tc.confirm, token, nil, nil, AuthRequestContext{}); !errors.Is(err, tc.want) {
				t.Fatalf("reset error = %v, want %v", err, tc.want)
			}
			verification, err := svc.repo.GetAuthVerificationByID(ctx, token)
			if err != nil || verification.UsedAt != nil {
				t.Fatalf("invalid password consumed token: %+v, err=%v", verification, err)
			}
			if err := svc.ResetPasswordWithOTP(ctx, phone, "NextPass123!", "NextPass123!", token, nil, nil, AuthRequestContext{}); err != nil {
				t.Fatalf("retry with corrected password: %v", err)
			}
		})
	}
}

func TestResetPasswordStorageFailureAllowsRetryWithSameToken(t *testing.T) {
	svc, db, phone, token := newPasswordResetService(t)
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TRIGGER fail_reset BEFORE UPDATE OF password ON users BEGIN SELECT RAISE(ABORT, 'simulated write failure'); END`); err != nil {
		t.Fatal(err)
	}
	reset := func() error {
		return svc.ResetPasswordWithOTP(ctx, phone, "NextPass123!", "NextPass123!", token, nil, nil, AuthRequestContext{})
	}
	if err := reset(); err == nil {
		t.Fatal("expected storage failure")
	}
	verification, err := svc.repo.GetAuthVerificationByID(ctx, token)
	if err != nil || verification.UsedAt != nil {
		t.Fatalf("failed write consumed token: %+v, err=%v", verification, err)
	}
	user, err := svc.repo.GetUserByPhone(ctx, phone)
	if err != nil || !comparePasswordHash(user.Password, "OriginalPass123!") {
		t.Fatalf("failed write changed original password: %v", err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_reset`); err != nil {
		t.Fatal(err)
	}
	if err := reset(); err != nil {
		t.Fatalf("retry with original token: %v", err)
	}
	user, err = svc.repo.GetUserByPhone(ctx, phone)
	if err != nil || !comparePasswordHash(user.Password, "NextPass123!") || comparePasswordHash(user.Password, "OriginalPass123!") {
		t.Fatalf("new password not installed after retry: %v", err)
	}
	if err := reset(); !errors.Is(err, ErrVerificationAlreadyUsed) {
		t.Fatalf("replay error = %v, want used token", err)
	}
}

func TestResetPasswordMissingUserDoesNotConsumeVerification(t *testing.T) {
	svc, db, phone, token := newPasswordResetService(t)
	ctx := context.Background()
	if _, err := db.Exec(`DELETE FROM users WHERE phone = ?`, phone); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		err := svc.ResetPasswordWithOTP(ctx, phone, "NextPass123!", "NextPass123!", token, nil, nil, AuthRequestContext{})
		if !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("reset error = %v, want user not found", err)
		}
		verification, err := svc.repo.GetAuthVerificationByID(ctx, token)
		if err != nil || verification.UsedAt != nil {
			t.Fatalf("missing user consumed token: %+v, err=%v", verification, err)
		}
	}
}
