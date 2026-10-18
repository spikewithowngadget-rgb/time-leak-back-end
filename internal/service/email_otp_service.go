package service

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"

	"time-leak/config"
	"time-leak/internal/domain"

	"go.uber.org/zap"
)

type EmailOTPRepository interface {
	OTPRepository
	CreateEmailOTPRequest(ctx context.Context, requestID, email string, purpose domain.AuthVerificationPurpose,
		codeHash string, expiresAt time.Time, maxAttempts int, cooldown time.Duration, dailyLimit int) (domain.OTPRequest, error)
	IncrementEmailOTPAttempt(ctx context.Context, requestID string) (int, error)
	MarkEmailOTPUsed(ctx context.Context, requestID string, usedAt time.Time) error
}

type emailOTPRepositoryAdapter struct {
	EmailOTPRepository
	cooldown   time.Duration
	dailyLimit int
}

func (a *emailOTPRepositoryAdapter) CreateOTPRequest(ctx context.Context, requestID string, channel domain.OTPChannel,
	destination string, purpose domain.AuthVerificationPurpose, codeHash string, expiresAt time.Time, maxAttempts int) (domain.OTPRequest, error) {
	if channel != domain.OTPChannelEmail {
		return domain.OTPRequest{}, ErrInvalidOTPChannel
	}
	return a.CreateEmailOTPRequest(ctx, requestID, destination, purpose, codeHash, expiresAt, maxAttempts, a.cooldown, a.dailyLimit)
}

func (a *emailOTPRepositoryAdapter) GetOTPRequestByID(ctx context.Context, id string) (domain.OTPRequest, error) {
	req, err := a.EmailOTPRepository.GetOTPRequestByID(ctx, id)
	if err != nil || req.Channel != domain.OTPChannelEmail {
		return req, err
	}
	latest, err := a.GetLatestOTPRequestByDestination(ctx, domain.OTPChannelEmail, req.Destination)
	if err != nil {
		return domain.OTPRequest{}, err
	}
	if latest.ID != req.ID {
		return domain.OTPRequest{}, sql.ErrNoRows
	}
	return req, nil
}

func (a *emailOTPRepositoryAdapter) IncrementOTPAttempt(ctx context.Context, id string) (int, error) {
	return a.IncrementEmailOTPAttempt(ctx, id)
}

func (a *emailOTPRepositoryAdapter) DeleteOTPRequest(ctx context.Context, id string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return a.EmailOTPRepository.DeleteOTPRequest(cleanupCtx, id)
}

func (a *emailOTPRepositoryAdapter) MarkOTPUsed(ctx context.Context, id string, usedAt time.Time) error {
	return a.MarkEmailOTPUsed(ctx, id, usedAt)
}

type EmailOTPService struct {
	core     *OTPService
	cooldown time.Duration
	sender   OTPSender
}

type unavailableEmailOTPSender struct{}

func (unavailableEmailOTPSender) SendOTP(context.Context, string, string) error {
	return ErrOTPDeliveryUnavailable
}
func (unavailableEmailOTPSender) Configured() bool { return false }

func NewEmailOTPService(repo EmailOTPRepository, otpCfg config.OTPConfig, emailCfg config.EmailOTPConfig,
	log *zap.Logger, sender OTPSender) *EmailOTPService {
	if emailCfg.RequestCooldown <= 0 {
		emailCfg.RequestCooldown = 32 * time.Second
	}
	if emailCfg.MaxRequestsPerDay <= 0 {
		emailCfg.MaxRequestsPerDay = 10
	}
	if sender == nil {
		sender = unavailableEmailOTPSender{}
	}
	adapter := &emailOTPRepositoryAdapter{EmailOTPRepository: repo, cooldown: emailCfg.RequestCooldown, dailyLimit: emailCfg.MaxRequestsPerDay}
	otpCfg.RequestCooldown = emailCfg.RequestCooldown
	otpCfg.TestEnabled = false
	core := NewOTPService(adapter, otpCfg, log, sender)
	core.channel = domain.OTPChannelEmail
	core.retainTestingCodes = false
	return &EmailOTPService{core: core, cooldown: emailCfg.RequestCooldown, sender: sender}
}

func (s *EmailOTPService) Request(ctx context.Context, email string) (domain.EmailOTPRequestResult, error) {
	email, valid := domain.NormalizeEmailAddress(email)
	if !valid {
		return domain.EmailOTPRequestResult{}, ErrInvalidOTPDestination
	}
	if checker, ok := s.sender.(interface{ Configured() bool }); ok && !checker.Configured() {
		return domain.EmailOTPRequestResult{}, ErrOTPDeliveryUnavailable
	}
	result, err := s.core.RequestOTP(ctx, domain.OTPChannelEmail, email)
	if err != nil {
		return domain.EmailOTPRequestResult{}, err
	}
	return domain.EmailOTPRequestResult{OTPRequestResult: result, Email: email, ResendAfterSeconds: int(math.Ceil(s.cooldown.Seconds()))}, nil
}

func (s *EmailOTPService) Verify(ctx context.Context, requestID, code string) (domain.EmailOTPVerifyResult, error) {
	requestID = strings.TrimSpace(requestID)
	result, err := s.core.VerifyOTP(ctx, requestID, code)
	if err != nil {
		return domain.EmailOTPVerifyResult{}, err
	}
	return domain.EmailOTPVerifyResult{RequestID: requestID, Email: result.Destination, Verified: true}, nil
}
