package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
	"time-leak/config"
	"time-leak/internal/domain"

	"go.uber.org/zap"
)

type otpRepoMock struct {
	requests map[string]domain.OTPRequest
	locks    map[string]domain.OTPLockState
}

func newOTPRepoMock() *otpRepoMock {
	return &otpRepoMock{
		requests: make(map[string]domain.OTPRequest),
		locks:    make(map[string]domain.OTPLockState),
	}
}

func (m *otpRepoMock) CreateOTPRequest(
	_ context.Context,
	requestID string,
	channel domain.OTPChannel,
	destination string,
	codeHash string,
	expiresAt time.Time,
	maxAttempts int,
) (domain.OTPRequest, error) {
	req := domain.OTPRequest{
		ID:          requestID,
		Channel:     channel,
		Destination: destination,
		CodeHash:    codeHash,
		ExpiresAt:   expiresAt,
		Attempts:    0,
		MaxAttempts: maxAttempts,
		CreatedAt:   time.Now().UTC(),
	}
	m.requests[requestID] = req
	return req, nil
}

func (m *otpRepoMock) GetOTPRequestByID(_ context.Context, requestID string) (domain.OTPRequest, error) {
	req, ok := m.requests[requestID]
	if !ok {
		return domain.OTPRequest{}, sql.ErrNoRows
	}
	return req, nil
}

func (m *otpRepoMock) GetLatestOTPRequestByDestination(
	_ context.Context,
	channel domain.OTPChannel,
	destination string,
) (domain.OTPRequest, error) {
	var latest domain.OTPRequest
	found := false
	for _, req := range m.requests {
		if req.Channel == channel && req.Destination == destination {
			if !found || req.CreatedAt.After(latest.CreatedAt) {
				latest = req
				found = true
			}
		}
	}
	if !found {
		return domain.OTPRequest{}, sql.ErrNoRows
	}
	return latest, nil
}

func (m *otpRepoMock) IncrementOTPAttempt(_ context.Context, requestID string) (int, error) {
	req, ok := m.requests[requestID]
	if !ok {
		return 0, sql.ErrNoRows
	}
	req.Attempts++
	now := time.Now().UTC()
	req.LastAttempt = &now
	m.requests[requestID] = req
	return req.Attempts, nil
}

func (m *otpRepoMock) MarkOTPUsed(_ context.Context, requestID string, usedAt time.Time) error {
	req, ok := m.requests[requestID]
	if !ok {
		return sql.ErrNoRows
	}
	if req.UsedAt != nil {
		return sql.ErrNoRows
	}
	req.UsedAt = &usedAt
	m.requests[requestID] = req
	return nil
}

func (m *otpRepoMock) GetOTPLockState(_ context.Context, channel domain.OTPChannel, destination string) (domain.OTPLockState, error) {
	state, ok := m.locks[string(channel)+":"+destination]
	if !ok {
		return domain.OTPLockState{}, sql.ErrNoRows
	}
	return state, nil
}

func (m *otpRepoMock) UpsertOTPLockState(_ context.Context, state domain.OTPLockState) error {
	m.locks[string(state.Channel)+":"+state.Destination] = state
	return nil
}

func (m *otpRepoMock) ResetOTPLockState(_ context.Context, channel domain.OTPChannel, destination string) error {
	m.locks[string(channel)+":"+destination] = domain.OTPLockState{
		Channel:        channel,
		Destination:    destination,
		FailedAttempts: 0,
		LockedUntil:    nil,
		UpdatedAt:      time.Now().UTC(),
	}
	return nil
}

func newOTPServiceForTest() (*OTPService, *otpRepoMock) {
	repo := newOTPRepoMock()
	svc := NewOTPService(repo, config.OTPConfig{
		HMACSecret:      "otp-test-secret",
		RequestCooldown: 1 * time.Second,
		MaxAttempts:     2,
		LockDuration:    5 * time.Minute,
		ExpiresIn:       60 * time.Second,
	}, zap.NewNop())
	return svc, repo
}

func TestOTP_Verify_Success(t *testing.T) {
	svc, repo := newOTPServiceForTest()
	ctx := context.Background()

	result, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if err != nil {
		t.Fatalf("RequestOTP error: %v", err)
	}
	if result.RequestID == "" {
		t.Fatal("expected request id")
	}

	debugCode, err := svc.GetLatestTestingOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if err != nil {
		t.Fatalf("GetLatestTestingOTP error: %v", err)
	}
	if len(debugCode.Code) != 4 {
		t.Fatalf("expected 4-digit OTP code, got %q", debugCode.Code)
	}

	verifyResult, err := svc.VerifyOTP(ctx, result.RequestID, debugCode.Code)
	if err != nil {
		t.Fatalf("VerifyOTP error: %v", err)
	}
	if verifyResult.Channel != domain.OTPChannelWhatsApp {
		t.Fatalf("unexpected channel: %q", verifyResult.Channel)
	}
	if verifyResult.Destination != "+77015556677" {
		t.Fatalf("unexpected destination: %q", verifyResult.Destination)
	}

	stored := repo.requests[result.RequestID]
	if stored.UsedAt == nil {
		t.Fatal("expected used_at to be set")
	}
}

func TestOTP_Verify_AttemptsAndLock(t *testing.T) {
	svc, _ := newOTPServiceForTest()
	ctx := context.Background()

	result, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if err != nil {
		t.Fatalf("RequestOTP error: %v", err)
	}

	debugCode, err := svc.GetLatestTestingOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if err != nil {
		t.Fatalf("GetLatestTestingOTP error: %v", err)
	}

	wrongCode := "9999"
	if wrongCode == debugCode.Code {
		wrongCode = "8888"
	}

	_, err = svc.VerifyOTP(ctx, result.RequestID, wrongCode)
	if !errors.Is(err, ErrOTPInvalidCode) {
		t.Fatalf("expected ErrOTPInvalidCode, got %v", err)
	}

	_, err = svc.VerifyOTP(ctx, result.RequestID, wrongCode)
	if !errors.Is(err, ErrOTPTooManyAttempts) {
		t.Fatalf("expected ErrOTPTooManyAttempts, got %v", err)
	}

	_, err = svc.VerifyOTP(ctx, result.RequestID, debugCode.Code)
	if !errors.Is(err, ErrOTPLocked) {
		t.Fatalf("expected ErrOTPLocked, got %v", err)
	}
}
