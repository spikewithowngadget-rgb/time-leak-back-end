package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"time-leak/config"
	"time-leak/internal/domain"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type requestAwareOTPSender interface {
	SendOTPForRequest(ctx context.Context, destination, code, requestID string) error
}

type ResendSender struct {
	baseURL, apiKey, from string
	ttl                   time.Duration
	client                *http.Client
	log                   *zap.Logger
}

func (s *ResendSender) Configured() bool { return s.apiKey != "" && s.from != "" }

func NewResendSender(cfg config.ResendConfig, otpTTL time.Duration, log *zap.Logger) *ResendSender {
	if log == nil {
		log = zap.NewNop()
	}
	if otpTTL < 3*time.Minute || otpTTL > 5*time.Minute {
		otpTTL = 5 * time.Minute
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.resend.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	from := ""
	if email, valid := domain.NormalizeEmailAddress(cfg.FromEmail); valid && !strings.ContainsAny(cfg.FromName, "\r\n") {
		from = (&mail.Address{Name: strings.TrimSpace(cfg.FromName), Address: email}).String()
	}
	return &ResendSender{baseURL: baseURL, apiKey: strings.TrimSpace(cfg.APIKey), from: from, ttl: otpTTL,
		client: &http.Client{Timeout: timeout}, log: log}
}

func (s *ResendSender) SendOTP(ctx context.Context, email, code string) error {
	return s.SendOTPForRequest(ctx, email, code, uuid.NewString())
}

func (s *ResendSender) SendOTPForRequest(ctx context.Context, email, code, requestID string) error {
	if !s.Configured() {
		return ErrOTPDeliveryUnavailable
	}
	email, valid := domain.NormalizeEmailAddress(email)
	if !valid {
		return ErrInvalidOTPDestination
	}
	if !isValidOTPCode(code) {
		return ErrOTPInvalidCode
	}
	message := otpMessageText(code, s.ttl)
	payload := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}{s.from, []string{email}, "Код подтверждения Time Leak", message}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return fmt.Errorf("%w: encode email", ErrOTPDeliveryFailed)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", &body)
	if err != nil {
		return fmt.Errorf("%w: build email request", ErrOTPDeliveryFailed)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "email-otp/"+requestID)
	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("resend otp request failed", zap.String("request_id", requestID))
		return fmt.Errorf("%w: email provider request failed", ErrOTPDeliveryFailed)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		s.log.Warn("resend otp rejected", zap.Int("status", resp.StatusCode), zap.String("request_id", requestID))
		return fmt.Errorf("%w: email provider status %d", ErrOTPDeliveryFailed, resp.StatusCode)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil || strings.TrimSpace(result.ID) == "" {
		return fmt.Errorf("%w: invalid email provider response", ErrOTPDeliveryFailed)
	}
	s.log.Info("resend otp accepted", zap.String("request_id", requestID))
	return nil
}
