package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"time-leak/config"
	"time-leak/internal/domain"
	dbtraits "time-leak/traits/database"

	"go.uber.org/zap"
)

var (
	ErrInvalidOTPChannel      = errors.New("invalid otp channel")
	ErrInvalidOTPDestination  = errors.New("invalid otp destination")
	ErrOTPTooManyRequests     = errors.New("otp request is too frequent")
	ErrOTPLocked              = errors.New("otp destination is temporarily locked")
	ErrOTPRequestNotFound     = errors.New("otp request not found")
	ErrOTPExpired             = errors.New("otp expired")
	ErrOTPAlreadyUsed         = errors.New("otp already used")
	ErrOTPInvalidCode         = errors.New("otp code is invalid")
	ErrOTPTooManyAttempts     = errors.New("otp attempts exceeded")
	ErrOTPTestingCodeNotFound = errors.New("otp testing code not found")
)

// appStoreTestPhone is the dedicated number for App Store / Play Market review
// engineers. The OTP for this number is always appStoreTestOTPCode so reviewers
// can complete registration without waiting for a real WhatsApp message.
// Only this single number gets the bypass; all other phones use random codes.
const (
	appStoreTestPhone   = "+77471231213"
	appStoreTestOTPCode = "1111"
)

type OTPRepository interface {
	CreateOTPRequest(
		ctx context.Context,
		requestID string,
		channel domain.OTPChannel,
		destination string,
		codeHash string,
		expiresAt time.Time,
		maxAttempts int,
	) (domain.OTPRequest, error)
	GetOTPRequestByID(ctx context.Context, requestID string) (domain.OTPRequest, error)
	GetLatestOTPRequestByDestination(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPRequest, error)
	IncrementOTPAttempt(ctx context.Context, requestID string) (int, error)
	MarkOTPUsed(ctx context.Context, requestID string, usedAt time.Time) error
	GetOTPLockState(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPLockState, error)
	UpsertOTPLockState(ctx context.Context, state domain.OTPLockState) error
	ResetOTPLockState(ctx context.Context, channel domain.OTPChannel, destination string) error
}

type OTPService struct {
	repo            OTPRepository
	hmacSecret      []byte
	requestCooldown time.Duration
	maxAttempts     int
	lockDuration    time.Duration
	otpTTL          time.Duration
	log             *zap.Logger
	testStore       *otpTestStore
}

type otpTestStore struct {
	mu   sync.RWMutex
	data map[string]domain.OTPTestingCode
}

func newOTPTestStore() *otpTestStore {
	return &otpTestStore{data: make(map[string]domain.OTPTestingCode)}
}

func (s *otpTestStore) set(item domain.OTPTestingCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[testStoreKey(item.Channel, item.Destination)] = item
}

func (s *otpTestStore) get(channel domain.OTPChannel, destination string) (domain.OTPTestingCode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.data[testStoreKey(channel, destination)]
	return item, ok
}

func (s *otpTestStore) delete(channel domain.OTPChannel, destination string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, testStoreKey(channel, destination))
}

func testStoreKey(channel domain.OTPChannel, destination string) string {
	return string(channel) + ":" + destination
}

func NewOTPService(repo OTPRepository, otpCfg config.OTPConfig, log *zap.Logger) *OTPService {
	if log == nil {
		log = zap.NewNop()
	}

	cooldown := otpCfg.RequestCooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}

	maxAttempts := otpCfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	lockDuration := otpCfg.LockDuration
	if lockDuration <= 0 {
		lockDuration = 2 * time.Minute
	}

	expiresIn := otpCfg.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 5 * time.Minute
	}
	if expiresIn < 3*time.Minute || expiresIn > 5*time.Minute {
		expiresIn = 5 * time.Minute
	}

	secret := strings.TrimSpace(otpCfg.HMACSecret)
	if secret == "" {
		secret = "change-me-otp"
	}

	return &OTPService{
		repo:            repo,
		hmacSecret:      []byte(secret),
		requestCooldown: cooldown,
		maxAttempts:     maxAttempts,
		lockDuration:    lockDuration,
		otpTTL:          expiresIn,
		log:             log,
		testStore:       newOTPTestStore(),
	}
}

func (s *OTPService) RequestOTP(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPRequestResult, error) {
	channel = normalizeOTPChannel(channel)
	destination = normalizeOTPDestination(channel, destination)
	if err := validateOTPDestination(channel, destination); err != nil {
		return domain.OTPRequestResult{}, err
	}

	now := time.Now().UTC()
	lockState, err := s.repo.GetOTPLockState(ctx, channel, destination)
	if err == nil && lockState.LockedUntil != nil && now.Before(*lockState.LockedUntil) {
		return domain.OTPRequestResult{}, ErrOTPLocked
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.OTPRequestResult{}, err
	}

	latest, err := s.repo.GetLatestOTPRequestByDestination(ctx, channel, destination)
	if err == nil {
		if latest.CreatedAt.Add(s.requestCooldown).After(now) {
			return domain.OTPRequestResult{}, ErrOTPTooManyRequests
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.OTPRequestResult{}, err
	}

	requestID := dbtraits.GenerateUUID()
	var code string
	if destination == appStoreTestPhone {
		// Fixed bypass code for App Store / Play Market review accounts.
		// Only this specific number uses this bypass — all other phones go through
		// the normal random code path.
		code = appStoreTestOTPCode
	} else {
		var genErr error
		code, genErr = generateNumericOTPCode()
		if genErr != nil {
			return domain.OTPRequestResult{}, genErr
		}
	}
	codeHash := s.hashOTP(requestID, channel, destination, code)
	expiresAt := now.Add(s.otpTTL)
	if _, err := s.repo.CreateOTPRequest(ctx, requestID, channel, destination, codeHash, expiresAt, s.maxAttempts); err != nil {
		return domain.OTPRequestResult{}, err
	}

	s.testStore.set(domain.OTPTestingCode{
		RequestID:   requestID,
		Channel:     channel,
		Destination: destination,
		Code:        code,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
	})

	return domain.OTPRequestResult{
		RequestID:        requestID,
		ExpiresInSeconds: int(s.otpTTL.Seconds()),
	}, nil
}

func (s *OTPService) VerifyOTP(ctx context.Context, requestID, code string) (domain.OTPVerifyResult, error) {
	requestID = strings.TrimSpace(requestID)
	code = strings.TrimSpace(code)
	if requestID == "" || !isValidOTPCode(code) {
		return domain.OTPVerifyResult{}, ErrOTPInvalidCode
	}

	req, err := s.repo.GetOTPRequestByID(ctx, requestID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.OTPVerifyResult{}, ErrOTPRequestNotFound
		}
		return domain.OTPVerifyResult{}, err
	}

	now := time.Now().UTC()
	lockState, lockErr := s.repo.GetOTPLockState(ctx, req.Channel, req.Destination)
	if lockErr == nil && lockState.LockedUntil != nil && now.Before(*lockState.LockedUntil) {
		return domain.OTPVerifyResult{}, ErrOTPLocked
	}
	if lockErr != nil && !errors.Is(lockErr, sql.ErrNoRows) {
		return domain.OTPVerifyResult{}, lockErr
	}

	if req.UsedAt != nil {
		return domain.OTPVerifyResult{}, ErrOTPAlreadyUsed
	}
	if now.After(req.ExpiresAt) {
		return domain.OTPVerifyResult{}, ErrOTPExpired
	}

	expectedHash := s.hashOTP(req.ID, req.Channel, req.Destination, code)
	if !constantTimeStringEqual(req.CodeHash, expectedHash) {
		attempts, incrementErr := s.repo.IncrementOTPAttempt(ctx, req.ID)
		if incrementErr != nil {
			return domain.OTPVerifyResult{}, incrementErr
		}

		state := domain.OTPLockState{
			Channel:     req.Channel,
			Destination: req.Destination,
		}
		if lockErr == nil {
			state = lockState
		}
		state.Channel = req.Channel
		state.Destination = req.Destination
		state.FailedAttempts++

		if attempts >= req.MaxAttempts || state.FailedAttempts >= s.maxAttempts {
			lockedUntil := now.Add(s.lockDuration)
			state.LockedUntil = &lockedUntil
			state.FailedAttempts = 0
			if err := s.repo.UpsertOTPLockState(ctx, state); err != nil {
				return domain.OTPVerifyResult{}, err
			}
			return domain.OTPVerifyResult{}, ErrOTPTooManyAttempts
		}

		if err := s.repo.UpsertOTPLockState(ctx, state); err != nil {
			return domain.OTPVerifyResult{}, err
		}
		return domain.OTPVerifyResult{}, ErrOTPInvalidCode
	}

	if err := s.repo.MarkOTPUsed(ctx, req.ID, now); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.OTPVerifyResult{}, ErrOTPAlreadyUsed
		}
		return domain.OTPVerifyResult{}, err
	}
	if err := s.repo.ResetOTPLockState(ctx, req.Channel, req.Destination); err != nil {
		return domain.OTPVerifyResult{}, err
	}

	return domain.OTPVerifyResult{Channel: req.Channel, Destination: req.Destination}, nil
}

func (s *OTPService) GetLatestTestingOTP(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPTestingCode, error) {
	_ = ctx
	channel = normalizeOTPChannel(channel)
	destination = normalizeOTPDestination(channel, destination)
	if err := validateOTPDestination(channel, destination); err != nil {
		return domain.OTPTestingCode{}, err
	}

	item, ok := s.testStore.get(channel, destination)
	if !ok {
		return domain.OTPTestingCode{}, ErrOTPTestingCodeNotFound
	}
	if time.Now().UTC().After(item.ExpiresAt) {
		s.testStore.delete(channel, destination)
		return domain.OTPTestingCode{}, ErrOTPTestingCodeNotFound
	}
	return item, nil
}

func (s *OTPService) hashOTP(requestID string, channel domain.OTPChannel, destination, code string) string {
	mac := hmac.New(sha256.New, s.hmacSecret)
	_, _ = mac.Write([]byte(requestID))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(string(channel)))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(destination))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeOTPChannel(channel domain.OTPChannel) domain.OTPChannel {
	switch strings.ToLower(strings.TrimSpace(string(channel))) {
	case string(domain.OTPChannelWhatsApp):
		return domain.OTPChannelWhatsApp
	default:
		return domain.OTPChannel("")
	}
}

func normalizeOTPDestination(channel domain.OTPChannel, destination string) string {
	switch channel {
	case domain.OTPChannelWhatsApp:
		return normalizePhone(destination)
	default:
		return strings.TrimSpace(destination)
	}
}

func validateOTPDestination(channel domain.OTPChannel, destination string) error {
	if destination == "" {
		return ErrInvalidOTPDestination
	}
	switch channel {
	case domain.OTPChannelWhatsApp:
		if !phoneE164Pattern.MatchString(destination) {
			return ErrInvalidOTPDestination
		}
		return nil
	default:
		return ErrInvalidOTPChannel
	}
}

func isValidOTPCode(code string) bool {
	if len(code) != 4 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func generateNumericOTPCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("otp random generation failed: %w", err)
	}
	n := binary.BigEndian.Uint32(b[:]) % 10000
	return fmt.Sprintf("%04d", n), nil
}

func constantTimeStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
