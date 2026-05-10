package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/repository"
	dbtraits "time-leak/traits/database"

	"go.uber.org/zap"
)

type TelegramOTPOpenInput struct {
	DeepLinkToken    string
	TelegramUserID   int64
	TelegramChatID   int64
	TelegramUsername string
	FirstName        string
	LastName         string
}

type TelegramOTPOpenResponse struct {
	RequestID           string `json:"request_id"`
	MaskedPhone         string `json:"masked_phone"`
	Purpose             string `json:"purpose"`
	Status              string `json:"status"`
	Language            string `json:"language"`
	ExpiresInSeconds    int    `json:"expires_in_seconds"`
	RequireContactMatch bool   `json:"require_contact_match"`
}

type TelegramOTPCodeSendInput struct {
	RequestID      string
	TelegramUserID int64
	ContactPhone   string
}

type TelegramOTPCodeSendResponse struct {
	RequestID        string `json:"request_id"`
	OTPCode          string `json:"otp_code"`
	MaskedPhone      string `json:"masked_phone"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	Language         string `json:"language"`
	Status           string `json:"status"`
}

type TelegramOTPRequestResponse struct {
	RequestID         string `json:"request_id"`
	TelegramDeepLink  string `json:"telegram_deep_link"`
	ExpiresInSeconds  int    `json:"expires_in_seconds"`
	Status            string `json:"status"`
}

type SecurityService struct {
	cfg *config.Config
	repo *repository.Repository
	otp IOTPService
	log *zap.Logger
}

func NewSecurityService(cfg *config.Config, repo *repository.Repository, otp IOTPService, log *zap.Logger) *SecurityService {
	if log == nil {
		log = zap.NewNop()
	}
	return &SecurityService{
		cfg: cfg,
		repo: repo,
		otp: otp,
		log: log,
	}
}

func (s *SecurityService) CreateTelegramOTPRequest(
	ctx context.Context,
	phone string,
	purpose string,
	device *AuthDeviceInput,
	location *AuthLocationInput,
	reqCtx AuthRequestContext,
) (TelegramOTPRequestResponse, error) {
	if strings.TrimSpace(s.cfg.TelegramOTP.BotUsername) == "" {
		return TelegramOTPRequestResponse{}, ErrTelegramBotNotReady
	}

	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return TelegramOTPRequestResponse{}, err
	}
	normalizedPurpose, err := normalizeAuthPurpose(purpose)
	if err != nil {
		return TelegramOTPRequestResponse{}, err
	}
	normalizedDevice, err := normalizeDeviceInput(device)
	if err != nil {
		return TelegramOTPRequestResponse{}, err
	}
	normalizedLocation, err := normalizeLocationInput(location)
	if err != nil {
		return TelegramOTPRequestResponse{}, err
	}

	requestID := dbtraits.GenerateUUID()
	sessionID := dbtraits.GenerateUUID()
	rawToken, err := newRandomToken(32)
	if err != nil {
		return TelegramOTPRequestResponse{}, err
	}
	expiresAt := time.Now().UTC().Add(s.cfg.TelegramOTP.ExpiresIn)
	session, err := s.repo.CreateTelegramOTPSession(ctx, domain.TelegramOTPSession{
		ID:                sessionID,
		RequestID:         requestID,
		Phone:             normalizedPhone,
		Purpose:           normalizedPurpose,
		DeepLinkTokenHash: s.hashDeepLinkToken(rawToken),
		Status:            domain.TelegramOTPSessionPending,
		Device:            normalizedDevice,
		Location:          normalizedLocation,
		ExpiresAt:         expiresAt,
	})
	if err != nil {
		return TelegramOTPRequestResponse{}, err
	}

	deviceID := ""
	if normalizedDevice != nil {
		deviceID = normalizedDevice.DeviceID
	}
	if err := s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
		Phone:        normalizedPhone,
		EventType:    "telegram_otp_requested",
		DeviceID:     deviceID,
		IPAddress:    reqCtx.IPAddress,
		UserAgent:    reqCtx.UserAgent,
		MetadataJSON: metadataWithRequestID(requestID, telegramRequestMetadata(normalizedPurpose, normalizedDevice, normalizedLocation)),
	}); err != nil {
		return TelegramOTPRequestResponse{}, err
	}
	if normalizedPurpose == domain.AuthVerificationPurposePasswordReset {
		_ = s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
			Phone:        normalizedPhone,
			EventType:    "password_reset_requested",
			DeviceID:     deviceID,
			IPAddress:    reqCtx.IPAddress,
			UserAgent:    reqCtx.UserAgent,
			MetadataJSON: metadataWithRequestID(requestID, nil),
		})
	}
	if normalizedLocation != nil {
		_ = s.repo.CreateUserLocationEvent(ctx, domain.UserLocationEvent{
			Phone:          normalizedPhone,
			DeviceID:       deviceID,
			EventType:      "otp_request",
			Latitude:       ptrFloat64(normalizedLocation.Latitude),
			Longitude:      ptrFloat64(normalizedLocation.Longitude),
			AccuracyMeters: normalizedLocation.AccuracyMeters,
			Source:         string(normalizedLocation.Source),
			IPAddress:      reqCtx.IPAddress,
			UserAgent:      reqCtx.UserAgent,
		})
	}

	return TelegramOTPRequestResponse{
		RequestID:        session.RequestID,
		TelegramDeepLink: s.buildDeepLink(rawToken),
		ExpiresInSeconds: int(time.Until(session.ExpiresAt).Seconds()),
		Status:           string(session.Status),
	}, nil
}

func (s *SecurityService) OpenTelegramOTPLink(ctx context.Context, in TelegramOTPOpenInput) (TelegramOTPOpenResponse, error) {
	token := strings.TrimSpace(in.DeepLinkToken)
	if token == "" {
		return TelegramOTPOpenResponse{}, ErrTelegramLinkInvalid
	}

	session, err := s.repo.GetTelegramOTPSessionByTokenHash(ctx, s.hashDeepLinkToken(token))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TelegramOTPOpenResponse{}, ErrTelegramLinkInvalid
		}
		return TelegramOTPOpenResponse{}, err
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.repo.UpdateTelegramOTPSessionExpired(ctx, session.RequestID)
		return TelegramOTPOpenResponse{}, ErrTelegramLinkExpired
	}
	if session.Status != domain.TelegramOTPSessionPending {
		return TelegramOTPOpenResponse{}, ErrTelegramLinkInvalid
	}

	session, err = s.repo.UpdateTelegramOTPSessionOpened(
		ctx,
		session.RequestID,
		in.TelegramUserID,
		in.TelegramChatID,
		in.TelegramUsername,
		in.FirstName,
		in.LastName,
		time.Now().UTC(),
	)
	if err != nil {
		return TelegramOTPOpenResponse{}, err
	}

	tgUserID := in.TelegramUserID
	_ = s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
		Phone:          session.Phone,
		EventType:      "telegram_otp_opened",
		TelegramUserID: &tgUserID,
		MetadataJSON:   metadataWithRequestID(session.RequestID, map[string]any{"purpose": session.Purpose}),
	})

	return TelegramOTPOpenResponse{
		RequestID:           session.RequestID,
		MaskedPhone:         maskPhone(session.Phone),
		Purpose:             string(session.Purpose),
		Status:              string(session.Status),
		Language:            s.resolveSessionLanguage(ctx, session.Phone),
		ExpiresInSeconds:    int(time.Until(session.ExpiresAt).Seconds()),
		RequireContactMatch: s.cfg.TelegramOTP.RequireContactMatch,
	}, nil
}

func (s *SecurityService) SendTelegramOTPCode(ctx context.Context, in TelegramOTPCodeSendInput) (TelegramOTPCodeSendResponse, error) {
	session, err := s.repo.GetTelegramOTPSessionByRequestID(ctx, in.RequestID)
	if err != nil {
		return TelegramOTPCodeSendResponse{}, err
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.repo.UpdateTelegramOTPSessionExpired(ctx, session.RequestID)
		return TelegramOTPCodeSendResponse{}, ErrTelegramLinkExpired
	}
	if session.Status != domain.TelegramOTPSessionOpened {
		return TelegramOTPCodeSendResponse{}, ErrTelegramSessionState
	}
	if session.TelegramUserID != nil && *session.TelegramUserID != in.TelegramUserID {
		return TelegramOTPCodeSendResponse{}, ErrTelegramSessionState
	}
	if s.cfg.TelegramOTP.RequireContactMatch {
		normalizedContact, err := validatePhoneStrict(in.ContactPhone)
		if err != nil || normalizedContact != session.Phone {
			return TelegramOTPCodeSendResponse{}, ErrContactPhoneMismatch
		}
	}

	result, code, err := s.otp.IssueOTPForRequest(ctx, domain.OTPChannelTelegram, session.Phone, session.Purpose, session.RequestID)
	if err != nil {
		return TelegramOTPCodeSendResponse{}, err
	}
	session, err = s.repo.UpdateTelegramOTPSessionCodeSent(ctx, session.RequestID, time.Now().UTC())
	if err != nil {
		return TelegramOTPCodeSendResponse{}, err
	}

	tgUserID := in.TelegramUserID
	_ = s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
		Phone:          session.Phone,
		EventType:      "telegram_otp_code_sent",
		TelegramUserID: &tgUserID,
		MetadataJSON:   metadataWithRequestID(session.RequestID, map[string]any{"purpose": session.Purpose, "channel": domain.OTPChannelTelegram}),
	})

	return TelegramOTPCodeSendResponse{
		RequestID:        session.RequestID,
		OTPCode:          code,
		MaskedPhone:      maskPhone(session.Phone),
		ExpiresInSeconds: result.ExpiresInSeconds,
		Language:         s.resolveSessionLanguage(ctx, session.Phone),
		Status:           string(session.Status),
	}, nil
}

func (s *SecurityService) CancelTelegramOTP(ctx context.Context, requestID string) error {
	return s.repo.UpdateTelegramOTPSessionCancelled(ctx, requestID, time.Now().UTC())
}

func (s *SecurityService) MarkTelegramOTPVerified(ctx context.Context, requestID string) error {
	if strings.TrimSpace(requestID) == "" {
		return nil
	}
	session, err := s.repo.GetTelegramOTPSessionByRequestID(ctx, requestID)
	if err != nil {
		return nil
	}
	if session.Status == domain.TelegramOTPSessionVerified {
		return nil
	}
	if err := s.repo.UpdateTelegramOTPSessionVerified(ctx, requestID, time.Now().UTC()); err != nil {
		return err
	}
	return s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
		Phone:        session.Phone,
		EventType:    "otp_verified",
		MetadataJSON: metadataWithRequestID(requestID, map[string]any{"purpose": session.Purpose, "channel": domain.OTPChannelTelegram}),
	})
}

func (s *SecurityService) ListTelegramOTPSessions(ctx context.Context, filter domain.TelegramOTPSessionListFilter) ([]domain.TelegramOTPSession, error) {
	return s.repo.ListTelegramOTPSessions(ctx, filter)
}

func (s *SecurityService) GetTelegramOTPSession(ctx context.Context, requestID string) (domain.TelegramOTPSession, error) {
	return s.repo.GetTelegramOTPSessionByRequestID(ctx, requestID)
}

func (s *SecurityService) buildDeepLink(rawToken string) string {
	username := strings.TrimPrefix(strings.TrimSpace(s.cfg.TelegramOTP.BotUsername), "@")
	return fmt.Sprintf("https://t.me/%s?start=otp_%s", username, rawToken)
}

func (s *SecurityService) hashDeepLinkToken(rawToken string) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(s.cfg.TelegramOTP.TokenSecret)))
	_, _ = mac.Write([]byte(strings.TrimSpace(rawToken)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *SecurityService) resolveSessionLanguage(ctx context.Context, phone string) string {
	user, err := s.repo.GetUserByPhone(ctx, phone)
	if err != nil || strings.TrimSpace(user.UserLanguage) == "" {
		return "en"
	}
	return user.UserLanguage
}

func telegramRequestMetadata(
	purpose domain.AuthVerificationPurpose,
	device *domain.AuthDevice,
	location *domain.AuthLocation,
) map[string]any {
	out := map[string]any{
		"purpose": string(purpose),
	}
	if device != nil {
		out["platform"] = device.Platform
		out["app_version"] = device.AppVersion
		out["os_version"] = device.OSVersion
		out["device_model"] = device.DeviceModel
		out["manufacturer"] = device.Manufacturer
	}
	if location != nil {
		out["location_source"] = location.Source
	}
	return out
}

func maskPhone(phone string) string {
	phone = normalizePhone(phone)
	if len(phone) <= 4 {
		return phone
	}
	runes := []rune(phone)
	for i := 2; i < len(runes)-2; i++ {
		if runes[i] >= '0' && runes[i] <= '9' {
			runes[i] = '*'
		}
	}
	return string(runes)
}
