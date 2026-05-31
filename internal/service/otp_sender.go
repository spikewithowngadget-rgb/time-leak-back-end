package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"time-leak/config"

	"go.uber.org/zap"
)

var (
	ErrOTPDeliveryUnavailable = errors.New("otp delivery is not configured")
	ErrOTPDeliveryFailed      = errors.New("otp delivery failed")
)

type OTPSender interface {
	SendOTP(ctx context.Context, phone string, code string) error
}

type noopOTPSender struct{}

func (noopOTPSender) SendOTP(context.Context, string, string) error {
	return nil
}

type WhapiSender struct {
	baseURL string
	token   string
	ttl     time.Duration
	client  *http.Client
	log     *zap.Logger
}

func NewWhapiSender(cfg config.WhapiConfig, otpTTL time.Duration, log *zap.Logger) *WhapiSender {
	if log == nil {
		log = zap.NewNop()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://whapi.kz"
	}
	return &WhapiSender{
		baseURL: baseURL,
		token:   strings.TrimSpace(cfg.Token),
		ttl:     otpTTL,
		client:  &http.Client{Timeout: timeout},
		log:     log,
	}
}

func (s *WhapiSender) SendOTP(ctx context.Context, phone string, code string) error {
	token := strings.TrimSpace(s.token)
	if token == "" {
		s.log.Warn("whapi token is not configured")
		return ErrOTPDeliveryUnavailable
	}

	normalizedPhone := normalizePhone(phone)
	if !phoneE164Pattern.MatchString(normalizedPhone) {
		return ErrInvalidOTPDestination
	}
	if !isValidOTPCode(strings.TrimSpace(code)) {
		return ErrOTPInvalidCode
	}

	payload := whapiMessageRequest{
		To:   strings.TrimPrefix(normalizedPhone, "+"),
		Text: otpMessageText(strings.TrimSpace(code), s.ttl),
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return fmt.Errorf("%w: encode request", ErrOTPDeliveryFailed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/message", &body)
	if err != nil {
		return fmt.Errorf("%w: build request", ErrOTPDeliveryFailed)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("whapi otp send failed",
			zap.String("phone", maskPhoneForLog(normalizedPhone)),
			zap.Error(err),
		)
		return fmt.Errorf("%w: provider request failed", ErrOTPDeliveryFailed)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		s.log.Warn("whapi otp send returned non-success",
			zap.String("phone", maskPhoneForLog(normalizedPhone)),
			zap.Int("status", resp.StatusCode),
		)
		return fmt.Errorf("%w: provider status %d", ErrOTPDeliveryFailed, resp.StatusCode)
	}

	s.log.Info("whapi otp sent",
		zap.String("phone", maskPhoneForLog(normalizedPhone)),
	)
	return nil
}

type whapiMessageRequest struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

func otpMessageText(code string, ttl time.Duration) string {
	minutes := int(ttl / time.Minute)
	if ttl%time.Minute != 0 {
		minutes++
	}
	if minutes <= 0 {
		minutes = 1
	}
	return fmt.Sprintf("Ваш код подтверждения Timeleak: %s. Код действителен %d минут.", code, minutes)
}

func maskPhoneForLog(phone string) string {
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
