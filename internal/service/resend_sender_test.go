package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"

	"time-leak/config"
)

func TestResendSender_RequestContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/emails" {
			t.Error("wrong Resend endpoint")
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Idempotency-Key") != "email-otp/test-request" {
			t.Error("wrong Resend headers")
		}
		var body struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		from, err := mail.ParseAddress(body.From)
		if err != nil || from.Address != "noreply@timeleak.kz" || from.Name != "Time Leak" || len(body.To) != 1 || body.To[0] != "person@example.com" || !strings.Contains(body.Text, "0123") || !strings.Contains(body.Text, "5 минут") || body.Subject == "" {
			t.Error("invalid email payload")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"email-provider-id"}`))
	}))
	defer server.Close()
	sender := NewResendSender(config.ResendConfig{APIKey: "test-key", FromEmail: "noreply@timeleak.kz", FromName: "Time Leak", BaseURL: server.URL}, 5*time.Minute, nil)
	if err := sender.SendOTPForRequest(context.Background(), " Person@Example.com ", "0123", "test-request"); err != nil {
		t.Fatal(err)
	}
}

func TestResendSender_ErrorsDoNotExposeProviderResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"unauthorized", 401, `{"message":"private-provider-secret"}`}, {"provider quota", 429, `{"message":"private-provider-secret"}`}, {"invalid success", 200, `{"message":"private-provider-secret"}`}, {"invalid JSON", 200, `bad-json`}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			sender := NewResendSender(config.ResendConfig{APIKey: "test-key", FromEmail: "noreply@timeleak.kz", BaseURL: server.URL}, 5*time.Minute, nil)
			err := sender.SendOTP(context.Background(), "person@example.com", "1234")
			if !errors.Is(err, ErrOTPDeliveryFailed) || strings.Contains(err.Error(), "private-provider-secret") || strings.Contains(err.Error(), "test-key") {
				t.Fatalf("unexpected delivery error: %v", err)
			}
		})
	}
}

func TestResendSender_TimeoutAndUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(60 * time.Millisecond):
		}
	}))
	defer server.Close()
	sender := NewResendSender(config.ResendConfig{APIKey: "test-key", FromEmail: "noreply@timeleak.kz", BaseURL: server.URL, Timeout: 20 * time.Millisecond}, 5*time.Minute, nil)
	if err := sender.SendOTP(context.Background(), "person@example.com", "1234"); !errors.Is(err, ErrOTPDeliveryFailed) {
		t.Fatalf("expected timeout: %v", err)
	}
	sender = NewResendSender(config.ResendConfig{}, 5*time.Minute, nil)
	if err := sender.SendOTP(context.Background(), "person@example.com", "1234"); !errors.Is(err, ErrOTPDeliveryUnavailable) {
		t.Fatalf("expected unconfigured sender: %v", err)
	}
}
