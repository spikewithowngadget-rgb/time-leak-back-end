package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"time-leak/internal/domain"
)

type ReminderDeliveryError struct {
	Code       string
	Retryable  bool
	RetryAfter time.Duration
}

func (e *ReminderDeliveryError) Error() string { return "email reminder delivery failed: " + e.Code }

// SendReminder shares the configured Resend client/sender with OTP, but keeps
// its payload, idempotency namespace, queue and retry policy independent.
func (s *ResendSender) SendReminder(ctx context.Context, delivery domain.ReminderDelivery) (string, error) {
	if !s.Configured() {
		return "", &ReminderDeliveryError{Code: "sender_unavailable"}
	}
	email, valid := domain.NormalizeEmailAddress(delivery.Email)
	if !valid || delivery.ID == "" || delivery.Subject == "" || delivery.Text == "" || delivery.HTML == "" {
		return "", &ReminderDeliveryError{Code: "invalid_payload"}
	}
	payload := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
		HTML    string   `json:"html"`
	}{s.from, []string{email}, delivery.Subject, delivery.Text, delivery.HTML}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return "", &ReminderDeliveryError{Code: "invalid_payload"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", &body)
	if err != nil {
		return "", &ReminderDeliveryError{Code: "invalid_request"}
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "timeleak-reminder/"+delivery.ID)
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", &ReminderDeliveryError{Code: "request_cancelled", Retryable: true}
		}
		return "", &ReminderDeliveryError{Code: "provider_network", Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var provider struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&provider)
		delay := resendRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		switch {
		case resp.StatusCode == 429:
			code := "provider_rate_limit"
			if strings.Contains(provider.Type, "quota") || strings.Contains(provider.Name, "quota") {
				code = "provider_quota"
				if delay == 0 {
					delay = time.Hour
				}
			}
			return "", &ReminderDeliveryError{Code: code, Retryable: true, RetryAfter: delay}
		case resp.StatusCode >= 500 || resp.StatusCode == 408:
			return "", &ReminderDeliveryError{Code: "provider_temporary", Retryable: true, RetryAfter: delay}
		case resp.StatusCode == 409 && (provider.Type == "concurrent_idempotent_requests" || provider.Name == "concurrent_idempotent_requests"):
			return "", &ReminderDeliveryError{Code: "provider_concurrent", Retryable: true, RetryAfter: delay}
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return "", &ReminderDeliveryError{Code: "provider_auth"}
		default:
			return "", &ReminderDeliveryError{Code: fmt.Sprintf("provider_rejected_%d", resp.StatusCode)}
		}
	}
	var result struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&result); err != nil || strings.TrimSpace(result.ID) == "" {
		return "", &ReminderDeliveryError{Code: "provider_response", Retryable: true}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", &ReminderDeliveryError{Code: "provider_response", Retryable: true}
	}
	return result.ID, nil
}
func resendRetryAfter(header string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds > 0 {
		// Clamp before multiplication so a large untrusted header cannot overflow
		// time.Duration into a negative retry interval.
		if seconds >= int((20*time.Hour)/time.Second) {
			return 20 * time.Hour
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(header); err == nil && date.After(now) {
		return min(date.Sub(now), 20*time.Hour)
	}
	return 0
}
