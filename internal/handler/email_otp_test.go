package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/repository"
	"time-leak/internal/service"
	"time-leak/traits/database"

	"go.uber.org/zap"
)

type emailOTPFixture struct {
	srv            *httptest.Server
	db             *sql.DB
	cfg            *config.Config
	svcs           *service.Services
	mu             sync.Mutex
	codes          map[string]string
	providerCalls  atomic.Int32
	providerStatus atomic.Int32
	providerDelay  atomic.Int64
}

func newEmailOTPFixture(t *testing.T, changeConfig func(*config.Config)) *emailOTPFixture {
	t.Helper()
	f := &emailOTPFixture{codes: make(map[string]string)}
	f.providerStatus.Store(200)
	codePattern := regexp.MustCompile(`[0-9]{4}`)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			To   json.RawMessage `json:"to"`
			Text string          `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var target string
		if r.URL.Path == "/emails" {
			if r.Header.Get("Authorization") != "Bearer test-resend-key" || r.Header.Get("Idempotency-Key") == "" {
				t.Error("missing email provider authentication or idempotency key")
			}
			var targets []string
			if err := json.Unmarshal(payload.To, &targets); err != nil || len(targets) != 1 {
				t.Error("wrong email recipients")
				return
			}
			target = targets[0]
			f.providerCalls.Add(1)
		} else if r.URL.Path == "/api/message" {
			if err := json.Unmarshal(payload.To, &target); err != nil {
				t.Error(err)
				return
			}
		} else {
			t.Error("wrong provider URL")
			w.WriteHeader(404)
			return
		}
		f.mu.Lock()
		f.codes[target] = codePattern.FindString(payload.Text)
		f.mu.Unlock()
		if delay := time.Duration(f.providerDelay.Load()); delay > 0 {
			select {
			case <-r.Context().Done():
			case <-time.After(delay):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(f.providerStatus.Load()))
		w.Write([]byte(`{"id":"provider-id","message":"provider-private-data"}`))
	}))
	t.Cleanup(provider.Close)
	tmp := t.TempDir()
	cfg := &config.Config{DBPath: tmp, DBName: "email-test.db", NoteFilesPath: filepath.Join(tmp, "files"), MaxOpenConns: 8, MaxIdleConns: 8,
		JWT:      config.JWTConfig{AccessSecret: "test-access", AdminSecret: "test-admin", AccessTTL: time.Minute, RefreshTTL: time.Hour},
		OTP:      config.OTPConfig{HMACSecret: "test-otp", RequestCooldown: time.Second, MaxAttempts: 5, LockDuration: 2 * time.Minute, ExpiresIn: 5 * time.Minute},
		EmailOTP: config.EmailOTPConfig{RequestCooldown: 32 * time.Second, MaxRequestsPerDay: 10},
		Resend:   config.ResendConfig{APIKey: "test-resend-key", FromEmail: "noreply@timeleak.kz", FromName: "Time Leak", BaseURL: provider.URL, Timeout: time.Second},
		Whapi:    config.WhapiConfig{Token: "test-whapi", BaseURL: provider.URL, Timeout: time.Second}, AppEnv: "test"}
	if changeConfig != nil {
		changeConfig(cfg)
	}
	db, err := database.InitDatabase(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f.db, f.cfg = db, cfg
	f.svcs = service.NewServices(context.Background(), cfg, repository.NewRepositories(db), zap.NewNop())
	mux := http.NewServeMux()
	New(f.svcs, cfg, zap.NewNop()).Register(mux)
	f.srv = httptest.NewServer(WithCORS(mux))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *emailOTPFixture) code(email string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.codes[email]
}

func (f *emailOTPFixture) request(t *testing.T, email string) domain.EmailOTPRequestResult {
	t.Helper()
	resp := doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/request", map[string]any{"email": email})
	requireNoteStatus(t, resp, 200)
	var result domain.EmailOTPRequestResult
	decodeJSON(t, resp, &result)
	return result
}

func (f *emailOTPFixture) verify(t *testing.T, id, code string, status int) *http.Response {
	t.Helper()
	resp := doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/verify", map[string]any{"request_id": id, "code": code})
	requireNoteStatus(t, resp, status)
	return resp
}

func (f *emailOTPFixture) ageCooldown(t *testing.T) {
	t.Helper()
	stamp := time.Now().UTC().Add(-33 * time.Second).Format("2006-01-02T15:04:05.000000000Z")
	if _, err := f.db.Exec(`UPDATE otp_requests SET created_at=? WHERE channel='email'`, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE email_otp_send_attempts SET requested_at=?`, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestEmailOTP_SendVerifyAndChannelIsolation(t *testing.T) {
	f := newEmailOTPFixture(t, nil)
	result := f.request(t, " Person+tag@Example.COM ")
	if result.Email != "person+tag@example.com" || result.RequestID == "" || result.ExpiresInSeconds != 300 || result.ResendAfterSeconds != 32 {
		t.Fatalf("invalid email request response: %+v", result)
	}
	code := f.code(result.Email)
	if len(code) != 4 {
		t.Fatal("email did not contain a four-digit code")
	}
	stored, err := repository.New(f.db).GetOTPRequestByID(context.Background(), result.RequestID)
	if err != nil || stored.Channel != domain.OTPChannelEmail || stored.CodeHash == code || len(stored.CodeHash) != 64 {
		t.Fatal("email OTP was not stored as a channel-bound hash")
	}
	if _, err := f.svcs.OTP.GetLatestTestingOTP(context.Background(), domain.OTPChannelEmail, result.Email); !errors.Is(err, service.ErrOTPTestingCodeNotFound) {
		t.Fatal("email code retained in testing store")
	}
	resp := doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{"request_id": result.RequestID, "code": code})
	requireNoteStatus(t, resp, 400)
	resp.Body.Close()
	resp = f.verify(t, result.RequestID, code, 200)
	var verified map[string]any
	decodeJSON(t, resp, &verified)
	if len(verified) != 3 || verified["verified"] != true || verified["email"] != result.Email || verified["request_id"] != result.RequestID {
		t.Fatalf("invalid verification result: %v", verified)
	}
	resp = f.verify(t, result.RequestID, code, 400)
	resp.Body.Close()
	phone := "+77015556677"
	resp = doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{"phone": phone})
	requireNoteStatus(t, resp, 200)
	var whatsapp domain.OTPRequestResult
	decodeJSON(t, resp, &whatsapp)
	resp = f.verify(t, whatsapp.RequestID, f.code(phone), 400)
	resp.Body.Close()
	resp = doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{"request_id": whatsapp.RequestID, "code": f.code(phone)})
	requireNoteStatus(t, resp, 200)
	resp.Body.Close()
}

func TestEmailOTP_CooldownDailyQuotaAndRestart(t *testing.T) {
	f := newEmailOTPFixture(t, func(cfg *config.Config) { cfg.EmailOTP.MaxRequestsPerDay = 2 })
	f.request(t, "person@example.com")
	resp := doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/request", map[string]any{"email": "PERSON@EXAMPLE.COM"})
	requireNoteStatus(t, resp, 429)
	if resp.Header.Get("Retry-After") == "" || resp.Header.Get("Access-Control-Expose-Headers") != "Retry-After" {
		t.Fatal("retry delay missing")
	}
	resp.Body.Close()
	f.ageCooldown(t)
	f.request(t, "person@example.com")
	f.ageCooldown(t)
	resp = doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/request", map[string]any{"email": "person@example.com"})
	requireNoteStatus(t, resp, 429)
	var quota struct {
		Error string `json:"error"`
		Retry int    `json:"retry_after_seconds"`
	}
	decodeJSON(t, resp, &quota)
	if !strings.Contains(quota.Error, "daily") || quota.Retry < 1 || quota.Retry > 86400 {
		t.Fatalf("invalid daily retry: %+v", quota)
	}
	// A new service instance has no in-memory state but must see the same quota.
	restarted := service.NewServices(context.Background(), f.cfg, repository.NewRepositories(f.db), zap.NewNop())
	_, err := restarted.EmailOTP.Request(context.Background(), "person@example.com")
	var rateErr *domain.EmailOTPRequestLimitError
	if !errors.As(err, &rateErr) || !rateErr.Daily {
		t.Fatal("restart cleared daily limit")
	}
	f.request(t, "other@example.com")
	if f.providerCalls.Load() != 3 {
		t.Fatal("rate-limited requests reached provider")
	}
	old := time.Now().UTC().Add(-25 * time.Hour).Format("2006-01-02T15:04:05.000000000Z")
	if _, err := f.db.Exec(`UPDATE email_otp_send_attempts SET requested_at=? WHERE email='person@example.com'`, old); err != nil {
		t.Fatal(err)
	}
	f.request(t, "person@example.com")
}

func TestEmailOTP_ProviderFailureKeepsQuotaButRemovesCode(t *testing.T) {
	f := newEmailOTPFixture(t, nil)
	f.providerStatus.Store(503)
	resp := doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/request", map[string]any{"email": "person@example.com"})
	requireNoteStatus(t, resp, 502)
	var body map[string]any
	decodeJSON(t, resp, &body)
	if body["error"] != "email otp delivery failed" {
		t.Fatal("provider error exposed private payload")
	}
	var requests, attempts int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM otp_requests WHERE channel='email'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM email_otp_send_attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if requests != 0 || attempts != 1 {
		t.Fatalf("failure cleanup: requests=%d attempts=%d", requests, attempts)
	}
	resp = doReq(t, f.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/request", map[string]any{"email": "person@example.com"})
	requireNoteStatus(t, resp, 429)
	resp.Body.Close()
	if f.providerCalls.Load() != 1 {
		t.Fatal("provider failure bypassed cooldown")
	}
}

func TestEmailOTP_CancelledDeliveryStillCleansUp(t *testing.T) {
	f := newEmailOTPFixture(t, nil)
	f.providerDelay.Store(int64(60 * time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := f.svcs.EmailOTP.Request(ctx, "person@example.com"); !errors.Is(err, service.ErrOTPDeliveryFailed) {
		t.Fatalf("expected cancelled delivery, got %v", err)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM otp_requests WHERE channel='email'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancelled request left active OTP: %d %v", count, err)
	}
}

func TestEmailOTP_InvalidPreviousExpiredAndExhaustedCodes(t *testing.T) {
	f := newEmailOTPFixture(t, nil)
	old := f.request(t, "person@example.com")
	oldCode := f.code(old.Email)
	f.ageCooldown(t)
	current := f.request(t, old.Email)
	code := f.code(old.Email)
	resp := f.verify(t, old.RequestID, oldCode, 400)
	resp.Body.Close()
	wrong := "0000"
	if code == wrong {
		wrong = "1111"
	}
	for i := 0; i < 5; i++ {
		status := 400
		if i == 4 {
			status = 429
		}
		resp = f.verify(t, current.RequestID, wrong, status)
		resp.Body.Close()
	}
	if _, err := f.db.Exec(`UPDATE otp_destination_locks SET locked_until=? WHERE channel='email'`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	resp = f.verify(t, current.RequestID, code, 429)
	resp.Body.Close()
	expired := f.request(t, "expired@example.com")
	if _, err := f.db.Exec(`UPDATE otp_requests SET expires_at=? WHERE id=?`, time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano), expired.RequestID); err != nil {
		t.Fatal(err)
	}
	resp = f.verify(t, expired.RequestID, f.code(expired.Email), 400)
	resp.Body.Close()
}

func TestEmailOTP_ConcurrentRequestsAndVerification(t *testing.T) {
	f := newEmailOTPFixture(t, nil)
	var accepted atomic.Int32
	var requestID string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.svcs.EmailOTP.Request(context.Background(), "person@example.com")
			if err == nil {
				accepted.Add(1)
				mu.Lock()
				requestID = result.RequestID
				mu.Unlock()
				return
			}
			var rateErr *domain.EmailOTPRequestLimitError
			if !errors.Is(err, service.ErrOTPTooManyRequests) && !errors.As(err, &rateErr) {
				t.Errorf("unexpected concurrent request error: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || f.providerCalls.Load() != 1 {
		t.Fatal("parallel requests bypassed cooldown")
	}
	code := f.code("person@example.com")
	accepted.Store(0)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svcs.EmailOTP.Verify(context.Background(), requestID, code)
			if err == nil {
				accepted.Add(1)
				return
			}
			if !errors.Is(err, service.ErrOTPAlreadyUsed) {
				t.Errorf("unexpected concurrent verify error: %v", err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatal("OTP was consumed more than once")
	}
}

func TestEmailOTP_ValidationAndMissingConfiguration(t *testing.T) {
	f := newEmailOTPFixture(t, nil)
	for _, body := range []string{`{}`, `{"email":"invalid"}`, `{"email":"Name <person@example.com>"}`, `{"email":"one@example.com,two@example.com"}`, `{"email":123}`, `{"email":"person@example.com","code":"1111"}`, `{"email":"person@example.com"} {}`, strings.Repeat(" ", 4097) + `{}`} {
		req, err := http.NewRequest(http.MethodPost, f.srv.URL+"/api/v1/auth/email-otp/request", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		requireNoteStatus(t, resp, 400)
		resp.Body.Close()
	}
	if f.providerCalls.Load() != 0 {
		t.Fatal("invalid request reached email provider")
	}
	missing := newEmailOTPFixture(t, func(cfg *config.Config) { cfg.Resend.APIKey = "" })
	resp := doReq(t, missing.srv.URL, http.MethodPost, "/api/v1/auth/email-otp/request", map[string]any{"email": "person@example.com"})
	requireNoteStatus(t, resp, 503)
	resp.Body.Close()
	var count int
	if err := missing.db.QueryRow(`SELECT COUNT(*) FROM email_otp_send_attempts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("missing configuration consumed email quota")
	}
}

func TestEmailOTP_SwaggerDocumentsBothChannelsInRussian(t *testing.T) {
	var spec struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]struct {
			Summary     string         `json:"summary"`
			Description string         `json:"description"`
			OperationID string         `json:"operationId"`
			Responses   map[string]any `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(swaggerSpec), &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Info.Version != "1.4.0" {
		t.Fatal("OTP Swagger version was not updated")
	}
	for _, path := range []string{"/api/v1/auth/otp/request", "/api/v1/auth/otp/verify", "/api/v1/auth/email-otp/request", "/api/v1/auth/email-otp/verify"} {
		operation, ok := spec.Paths[path]["post"]
		if !ok || operation.OperationID == "" || operation.Responses["200"] == nil || operation.Responses["400"] == nil || operation.Responses["429"] == nil {
			t.Fatalf("incomplete OTP operation: %s", path)
		}
		if !strings.ContainsFunc(operation.Summary, func(r rune) bool { return unicode.In(r, unicode.Cyrillic) }) || !strings.ContainsFunc(operation.Description, func(r rune) bool { return unicode.In(r, unicode.Cyrillic) }) {
			t.Fatalf("missing Russian OTP explanation: %s", path)
		}
	}
}
