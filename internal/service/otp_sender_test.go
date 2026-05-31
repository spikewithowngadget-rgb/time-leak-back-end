package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"time-leak/config"

	"go.uber.org/zap"
)

func TestWhapiSender_SendOTP_PostsExpectedJSON(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody whapiMessageRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type: got %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode whapi body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewWhapiSender(config.WhapiConfig{
		BaseURL: srv.URL,
		Token:   "redacted-test-token",
		Timeout: time.Second,
	}, 5*time.Minute, zap.NewNop())

	if err := sender.SendOTP(context.Background(), "+77471850499", "1234"); err != nil {
		t.Fatalf("SendOTP error: %v", err)
	}
	if gotPath != "/api/message" {
		t.Fatalf("path: got %q want /api/message", gotPath)
	}
	if gotAuth != "Bearer redacted-test-token" {
		t.Fatalf("authorization header mismatch")
	}
	if gotBody.To != "77471850499" {
		t.Fatalf("to: got %q want 77471850499", gotBody.To)
	}
	wantText := "Ваш код подтверждения Timeleak: 1234. Код действителен 5 минут."
	if gotBody.Text != wantText {
		t.Fatalf("text: got %q want %q", gotBody.Text, wantText)
	}
}

func TestWhapiSender_SendOTP_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	sender := NewWhapiSender(config.WhapiConfig{
		BaseURL: srv.URL,
		Token:   "redacted-test-token",
		Timeout: time.Second,
	}, 5*time.Minute, zap.NewNop())

	err := sender.SendOTP(context.Background(), "+77471850499", "1234")
	if !errors.Is(err, ErrOTPDeliveryFailed) {
		t.Fatalf("expected ErrOTPDeliveryFailed, got %v", err)
	}
}

func TestWhapiSender_SendOTP_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewWhapiSender(config.WhapiConfig{
		BaseURL: srv.URL,
		Token:   "redacted-test-token",
		Timeout: time.Millisecond,
	}, 5*time.Minute, zap.NewNop())

	err := sender.SendOTP(context.Background(), "+77471850499", "1234")
	if !errors.Is(err, ErrOTPDeliveryFailed) {
		t.Fatalf("expected ErrOTPDeliveryFailed, got %v", err)
	}
}
