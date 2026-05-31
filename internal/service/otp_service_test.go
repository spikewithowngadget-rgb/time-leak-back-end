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
	requests  map[string]domain.OTPRequest
	locks     map[string]domain.OTPLockState
	createErr error
}

type otpSenderMock struct {
	calls int
	phone string
	code  string
	err   error
}

func (m *otpSenderMock) SendOTP(_ context.Context, phone string, code string) error {
	m.calls++
	m.phone = phone
	m.code = code
	return m.err
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
	purpose domain.AuthVerificationPurpose,
	codeHash string,
	expiresAt time.Time,
	maxAttempts int,
) (domain.OTPRequest, error) {
	if m.createErr != nil {
		return domain.OTPRequest{}, m.createErr
	}
	req := domain.OTPRequest{
		ID:          requestID,
		Channel:     channel,
		Destination: destination,
		Purpose:     purpose,
		CodeHash:    codeHash,
		ExpiresAt:   expiresAt,
		Attempts:    0,
		MaxAttempts: maxAttempts,
		CreatedAt:   time.Now().UTC(),
	}
	m.requests[requestID] = req
	return req, nil
}

func (m *otpRepoMock) DeleteOTPRequest(_ context.Context, requestID string) error {
	if _, ok := m.requests[requestID]; !ok {
		return sql.ErrNoRows
	}
	delete(m.requests, requestID)
	return nil
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
	if stored.CodeHash == debugCode.Code {
		t.Fatal("stored OTP must be hashed, not plaintext")
	}
	if len(stored.CodeHash) != 64 {
		t.Fatalf("expected sha256 hex hash length 64, got %d", len(stored.CodeHash))
	}
}

func TestOTP_Verify_WrongCodeFails(t *testing.T) {
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

func TestOTP_Verify_ExpiredFails(t *testing.T) {
	svc, repo := newOTPServiceForTest()
	ctx := context.Background()

	result, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if err != nil {
		t.Fatalf("RequestOTP error: %v", err)
	}
	req := repo.requests[result.RequestID]
	req.ExpiresAt = time.Now().UTC().Add(-time.Second)
	repo.requests[result.RequestID] = req

	debugCode, err := svc.GetLatestTestingOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if err != nil {
		t.Fatalf("GetLatestTestingOTP error: %v", err)
	}

	_, err = svc.VerifyOTP(ctx, result.RequestID, debugCode.Code)
	if !errors.Is(err, ErrOTPExpired) {
		t.Fatalf("expected ErrOTPExpired, got %v", err)
	}
}

func TestOTP_Request_RespectsRateLimit(t *testing.T) {
	svc, _ := newOTPServiceForTest()
	ctx := context.Background()

	if _, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677"); err != nil {
		t.Fatalf("first RequestOTP error: %v", err)
	}
	_, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77015556677")
	if !errors.Is(err, ErrOTPTooManyRequests) {
		t.Fatalf("expected ErrOTPTooManyRequests, got %v", err)
	}
}

func TestOTP_Request_DoesNotSendWhenDBSaveFails(t *testing.T) {
	repo := newOTPRepoMock()
	repo.createErr = errors.New("create failed")
	sender := &otpSenderMock{}
	svc := NewOTPService(repo, config.OTPConfig{
		HMACSecret:      "otp-test-secret",
		RequestCooldown: time.Second,
		MaxAttempts:     2,
		LockDuration:    5 * time.Minute,
		ExpiresIn:       time.Minute,
	}, zap.NewNop(), sender)

	_, err := svc.RequestOTP(context.Background(), domain.OTPChannelWhatsApp, "+77015556677")
	if err == nil {
		t.Fatal("expected create error")
	}
	if sender.calls != 0 {
		t.Fatalf("sender called %d times; want 0", sender.calls)
	}
}

func TestOTP_Request_DeletesStoredOTPWhenDeliveryFails(t *testing.T) {
	repo := newOTPRepoMock()
	sender := &otpSenderMock{err: ErrOTPDeliveryFailed}
	svc := NewOTPService(repo, config.OTPConfig{
		HMACSecret:      "otp-test-secret",
		RequestCooldown: time.Second,
		MaxAttempts:     2,
		LockDuration:    5 * time.Minute,
		ExpiresIn:       time.Minute,
	}, zap.NewNop(), sender)

	_, err := svc.RequestOTP(context.Background(), domain.OTPChannelWhatsApp, "+77015556677")
	if !errors.Is(err, ErrOTPDeliveryFailed) {
		t.Fatalf("expected ErrOTPDeliveryFailed, got %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("sender calls: got %d want 1", sender.calls)
	}
	if len(repo.requests) != 0 {
		t.Fatalf("expected failed delivery OTP to be deleted, stored requests: %d", len(repo.requests))
	}
}

func TestOTP_Request_StaticTestCodeRequiresConfig(t *testing.T) {
	repo := newOTPRepoMock()
	sender := &otpSenderMock{err: ErrOTPDeliveryFailed}
	svc := NewOTPService(repo, config.OTPConfig{
		HMACSecret:      "otp-test-secret",
		RequestCooldown: time.Second,
		MaxAttempts:     2,
		LockDuration:    5 * time.Minute,
		ExpiresIn:       time.Minute,
		TestEnabled:     true,
		TestPhone:       "77471850499",
		TestCode:        "1111",
		AppEnv:          "development",
	}, zap.NewNop(), sender)
	ctx := context.Background()

	result, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77471850499")
	if err != nil {
		t.Fatalf("RequestOTP error: %v", err)
	}
	debugCode, err := svc.GetLatestTestingOTP(ctx, domain.OTPChannelWhatsApp, "+77471850499")
	if err != nil {
		t.Fatalf("GetLatestTestingOTP error: %v", err)
	}
	if debugCode.Code != "1111" {
		t.Fatalf("expected configured static code 1111 got %q", debugCode.Code)
	}
	if sender.calls != 0 {
		t.Fatalf("static test OTP should not call sender, got %d calls", sender.calls)
	}
	if _, err := svc.VerifyOTP(ctx, result.RequestID, "1111"); err != nil {
		t.Fatalf("VerifyOTP with static code error: %v", err)
	}
}

func TestOTP_Request_StaticTestCodeDisabledByDefault(t *testing.T) {
	svc, _ := newOTPServiceForTest()
	ctx := context.Background()

	result, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77471850499")
	if err != nil {
		t.Fatalf("RequestOTP error: %v", err)
	}
	debugCode, err := svc.GetLatestTestingOTP(ctx, domain.OTPChannelWhatsApp, "+77471850499")
	if err != nil {
		t.Fatalf("GetLatestTestingOTP error: %v", err)
	}
	wrongCode := "1111"
	if wrongCode == debugCode.Code {
		wrongCode = "2222"
	}
	_, err = svc.VerifyOTP(ctx, result.RequestID, wrongCode)
	if !errors.Is(err, ErrOTPInvalidCode) {
		t.Fatalf("expected ErrOTPInvalidCode, got %v", err)
	}
}

func TestOTP_Request_StaticTestCodeDisabledInProduction(t *testing.T) {
	repo := newOTPRepoMock()
	sender := &otpSenderMock{}
	svc := NewOTPService(repo, config.OTPConfig{
		HMACSecret:      "otp-test-secret",
		RequestCooldown: time.Second,
		MaxAttempts:     2,
		LockDuration:    5 * time.Minute,
		ExpiresIn:       time.Minute,
		TestEnabled:     true,
		TestPhone:       "77471850499",
		TestCode:        "1111",
		AppEnv:          "production",
	}, zap.NewNop(), sender)
	ctx := context.Background()

	result, err := svc.RequestOTP(ctx, domain.OTPChannelWhatsApp, "+77471850499")
	if err != nil {
		t.Fatalf("RequestOTP error: %v", err)
	}
	if sender.calls != 1 {
		t.Fatalf("production static config must send real OTP, sender calls: got %d want 1", sender.calls)
	}
	debugCode, err := svc.GetLatestTestingOTP(ctx, domain.OTPChannelWhatsApp, "+77471850499")
	if err != nil {
		t.Fatalf("GetLatestTestingOTP error: %v", err)
	}
	wrongCode := "1111"
	if wrongCode == debugCode.Code {
		wrongCode = "2222"
	}
	_, err = svc.VerifyOTP(ctx, result.RequestID, wrongCode)
	if !errors.Is(err, ErrOTPInvalidCode) {
		t.Fatalf("expected ErrOTPInvalidCode, got %v", err)
	}
}

func TestOTP_StaticTestConfigDoesNotAllowOldGarbageNumbers(t *testing.T) {
	repo := newOTPRepoMock()
	svc := NewOTPService(repo, config.OTPConfig{
		HMACSecret:      "otp-test-secret",
		RequestCooldown: time.Second,
		MaxAttempts:     2,
		LockDuration:    5 * time.Minute,
		ExpiresIn:       time.Minute,
		TestEnabled:     true,
		TestPhone:       "77471850499",
		TestCode:        "1111",
		AppEnv:          "development",
	}, zap.NewNop())

	oldNumbers := []string{
		"+77081234000",
		"+77081234001",
		"+77071234002",
		"+77051234003",
		"+77011234004",
		"+77021234005",
		"+77751234006",
		"+77771234007",
		"+77061234008",
		"+77781234009",
		"+77081234010",
		"+77071234011",
		"+77051234012",
		"+77011234013",
		"+77021234014",
		"+77751234015",
		"+77771234016",
		"+77061234017",
		"+77781234018",
		"+77471234019",
		"+77471231213",
	}
	for _, phone := range oldNumbers {
		if code, ok := svc.lookupStaticTestingOTP(phone); ok {
			t.Fatalf("old testing phone %s unexpectedly bypassed with code %s", phone, code)
		}
	}
}
