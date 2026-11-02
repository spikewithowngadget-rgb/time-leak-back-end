package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"time-leak/internal/service"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type passwordResetAppStub struct {
	service.IUserNotesService
	err    error
	called bool
}

func (s *passwordResetAppStub) ResetPasswordWithOTP(context.Context, string, string, string, string, *service.AuthDeviceInput, *service.AuthLocationInput, service.AuthRequestContext) error {
	s.called = true
	return s.err
}

const resetTestBody = `{"phone":"+77015556677","new_password":"PrivateNewPassword123!","confirm_password":"PrivateNewPassword123!","verification_token":"private-verification-token"}`

func requireResetLogPrivacy(t *testing.T, entries []observer.LoggedEntry) {
	t.Helper()
	for _, entry := range entries {
		encoded, err := json.Marshal(entry.ContextMap())
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"+77015556677", "PrivateNewPassword123!", "private-verification-token"} {
			if strings.Contains(string(encoded), secret) || strings.Contains(entry.Message, secret) {
				t.Fatalf("password reset log contains private request data: %s", encoded)
			}
		}
	}
}

func TestPasswordResetConfirmDiagnosticErrors(t *testing.T) {
	cases := []struct {
		name, code, message string
		err                 error
		status              int
	}{
		{"phone required", "phone_required", "phone is required", service.ErrPhoneRequired, 400},
		{"invalid phone", "invalid_phone", "phone format is invalid", service.ErrInvalidPhoneFormat, 400},
		{"password required", "password_required", "password is required", service.ErrPasswordRequired, 400},
		{"password mismatch", "password_mismatch", "passwords do not match", service.ErrPasswordMismatch, 400},
		{"password too short", "password_too_short", "password must be at least 8 characters", service.ErrPasswordTooShort, 400},
		{"password too long", "password_too_long", "password must not exceed 72 bytes", service.ErrPasswordTooLong, 400},
		{"verification missing", "verification_token_missing", "verification token is required", service.ErrVerificationTokenMissing, 400},
		{"verification not found", "verification_token_not_found", "verification token not found", service.ErrVerificationNotFound, 400},
		{"verification expired", "verification_token_expired", "verification token expired", service.ErrVerificationExpired, 400},
		{"verification used", "verification_token_used", "verification token already used", service.ErrVerificationAlreadyUsed, 400},
		{"verification invalid", "verification_token_invalid", "verification token is invalid", service.ErrVerificationInvalid, 400},
		{"user missing", "user_not_found", "user not found", service.ErrUserNotFound, 404},
		{"internal failure", "internal_error", "internal", errors.New("storage unavailable"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &passwordResetAppStub{err: fmt.Errorf("wrapped: %w", tc.err)}
			core, logs := observer.New(zapcore.DebugLevel)
			h := New(&service.Services{App: app}, nil, zap.New(core))
			response := httptest.NewRecorder()
			h.AuthPasswordResetConfirm(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/password-reset/confirm", strings.NewReader(resetTestBody)))
			if !app.called {
				t.Fatal("reset service was not called")
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != tc.status || body["code"] != tc.code || body["error"] != tc.message {
				t.Fatalf("status=%d body=%v, want status=%d code=%q error=%q", response.Code, body, tc.status, tc.code, tc.message)
			}
			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("got %d log entries, want 1", len(entries))
			}
			level := zapcore.WarnLevel
			if tc.status == 500 {
				level = zapcore.ErrorLevel
				if !strings.Contains(fmt.Sprint(entries[0].ContextMap()["error"]), "storage unavailable") {
					t.Fatal("internal error cause missing from server log")
				}
			}
			fields := entries[0].ContextMap()
			if entries[0].Level != level || fields["reason"] != tc.code || fields["status"] != int64(tc.status) {
				t.Fatalf("wrong reset log: %+v", entries[0])
			}
			requireResetLogPrivacy(t, entries)
		})
	}
}

func TestPasswordResetConfirmInvalidJSONDiagnostic(t *testing.T) {
	app := &passwordResetAppStub{}
	core, logs := observer.New(zapcore.DebugLevel)
	h := New(&service.Services{App: app}, nil, zap.New(core))
	response := httptest.NewRecorder()
	h.AuthPasswordResetConfirm(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/password-reset/confirm", strings.NewReader(`{"phone":"+77015556677","private-verification-token":`)))
	if app.called {
		t.Fatal("invalid JSON reached password reset service")
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 400 || body["code"] != "invalid_payload" || body["error"] != "bad request" {
		t.Fatalf("unexpected malformed JSON response: status=%d body=%v", response.Code, body)
	}
	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zapcore.WarnLevel || entries[0].Message != "password reset rejected" || entries[0].ContextMap()["reason"] != "invalid_payload" {
		t.Fatalf("unexpected malformed JSON log: %+v", entries)
	}
	requireResetLogPrivacy(t, entries)
}

func TestPasswordResetConfirmSuccessLog(t *testing.T) {
	app := &passwordResetAppStub{}
	core, logs := observer.New(zapcore.DebugLevel)
	h := New(&service.Services{App: app}, nil, zap.New(core))
	response := httptest.NewRecorder()
	h.AuthPasswordResetConfirm(response, httptest.NewRequest(http.MethodPost, "/api/v1/auth/password-reset/confirm", strings.NewReader(resetTestBody)))
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !app.called || response.Code != 200 || body["status"] != "password_updated" {
		t.Fatalf("unexpected success response: status=%d body=%v", response.Code, body)
	}
	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zapcore.InfoLevel || entries[0].Message != "password reset completed" {
		t.Fatalf("unexpected success log: %+v", entries)
	}
	requireResetLogPrivacy(t, entries)
}
