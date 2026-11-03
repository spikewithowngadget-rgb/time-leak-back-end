package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"

	"time-leak/config"
	"time-leak/internal/domain"
)

func reminderProviderFixture() domain.ReminderDelivery {
	return domain.ReminderDelivery{
		ID: strings.Repeat("ab", 32), Email: "person@example.test", Title: "Unrendered note title",
		Language: "ru", DaysBefore: 3, OccurrenceDate: "2026-11-07",
		Subject: "TimeLeak: День рождения — через 3 дня",
		Text:    "Frozen text: \"friend\", <special> & characters\nSecond line.\u2028中文 العربية",
		HTML:    `<html lang="ru"><body><p>Frozen &lt;special&gt; &amp; \"friend\"</p></body></html>`,
	}
}

func reminderTestSender(baseURL string) *ResendSender {
	return NewResendSender(config.ResendConfig{APIKey: "re_synthetic_reminder_key", FromEmail: "noreply@timeleak.kz", FromName: "TimeLeak Reminders", BaseURL: baseURL}, 5*time.Minute, nil)
}

func TestResendReminderFrozenPayloadAndIdempotencyContract(t *testing.T) {
	delivery := reminderProviderFixture()
	var firstBody []byte
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("unexpected provider endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer re_synthetic_reminder_key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("provider request must use configured Resend credentials and JSON")
		}
		if key := r.Header.Get("Idempotency-Key"); key != "timeleak-reminder/"+delivery.ID || strings.HasPrefix(key, "email-otp/") || len(key) > 256 {
			t.Errorf("invalid reminder idempotency namespace: %q", key)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if requests == 1 {
			firstBody = append([]byte(nil), body...)
		} else if !bytes.Equal(firstBody, body) {
			t.Error("retry changed the frozen provider payload")
		}
		var payload struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
			HTML    string   `json:"html"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		from, err := mail.ParseAddress(payload.From)
		if err != nil || from.Address != "noreply@timeleak.kz" || from.Name != "TimeLeak Reminders" {
			t.Error("provider sender must match existing Resend configuration")
		}
		if len(payload.To) != 1 || payload.To[0] != delivery.Email || payload.Subject != delivery.Subject || payload.Text != delivery.Text || payload.HTML != delivery.HTML {
			t.Error("provider request did not preserve the frozen recipient/message fields")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil || len(fields) != 5 {
			t.Error("reminder payload unexpectedly contains additional user data")
		}
		if strings.Contains(string(body), "re_synthetic_reminder_key") || bytes.Contains(body, []byte("<special>")) {
			t.Error("JSON encoding/credential separation failed")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"provider-delivery-1"}`))
	}))
	defer server.Close()
	sender := reminderTestSender(server.URL)
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			delivery.Title = "Current title edited after queueing"
			delivery.Language = "kk"
			delivery.DaysBefore = 2
		}
		id, err := sender.SendReminder(context.Background(), delivery)
		if err != nil || id != "provider-delivery-1" {
			t.Fatalf("send id=%q err=%v", id, err)
		}
	}
	if requests != 2 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestResendReminderClassifiesProviderFailuresWithoutLeakingPayload(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		status                 int
		body, retryAfter, code string
		retryable              bool
		delay                  time.Duration
	}{
		{"rate limit", 429, `{"name":"rate_limit_exceeded","message":"private_provider_response"}`, "120", "provider_rate_limit", true, 2 * time.Minute},
		{"daily quota", 429, `{"name":"daily_quota_exceeded","message":"private_provider_response"}`, "", "provider_quota", true, time.Hour},
		{"monthly quota type", 429, `{"type":"monthly_quota_exceeded","message":"private_provider_response"}`, "600", "provider_quota", true, 10 * time.Minute},
		{"unparseable rate error", 429, `private_provider_response`, "", "provider_rate_limit", true, 0},
		{"server unavailable", 503, `{"message":"private_provider_response"}`, "30", "provider_temporary", true, 30 * time.Second},
		{"server error", 500, `{"message":"private_provider_response"}`, "", "provider_temporary", true, 0},
		{"timeout response", 408, `{"message":"private_provider_response"}`, "", "provider_temporary", true, 0},
		{"missing key", 401, `{"name":"missing_api_key","message":"private_provider_response"}`, "", "provider_auth", false, 0},
		{"unverified domain", 403, `{"name":"validation_error","message":"private_provider_response"}`, "", "provider_auth", false, 0},
		{"invalid request", 400, `{"name":"validation_error","message":"private_provider_response"}`, "", "provider_rejected_400", false, 0},
		{"invalid parameter", 422, `{"name":"invalid_parameter","message":"private_provider_response"}`, "", "provider_rejected_422", false, 0},
		{"concurrent request", 409, `{"name":"concurrent_idempotent_requests","message":"private_provider_response"}`, "5", "provider_concurrent", true, 5 * time.Second},
		{"concurrent request type", 409, `{"type":"concurrent_idempotent_requests"}`, "", "provider_concurrent", true, 0},
		{"changed idempotency payload", 409, `{"name":"invalid_idempotent_request","message":"private_provider_response"}`, "", "provider_rejected_409", false, 0},
		{"unknown conflict", 409, `{"name":"unknown","message":"private_provider_response"}`, "", "provider_rejected_409", false, 0},
		{"invalid success JSON", 200, `private_provider_response`, "", "provider_response", true, 0},
		{"missing success ID", 200, `{"message":"private_provider_response"}`, "", "provider_response", true, 0},
		{"blank success ID", 200, `{"id":"   "}`, "", "provider_response", true, 0},
		{"invalid ID type", 200, `{"id":42}`, "", "provider_response", true, 0},
		{"trailing garbage", 200, `{"id":"accepted"} private_provider_response`, "", "provider_response", true, 0},
		{"multiple success objects", 200, `{"id":"accepted"}{"id":"another"}`, "", "provider_response", true, 0},
		{"empty success", 204, ``, "", "provider_response", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			id, err := reminderTestSender(server.URL).SendReminder(context.Background(), reminderProviderFixture())
			var classified *ReminderDeliveryError
			if id != "" || !errors.As(err, &classified) || classified.Code != tc.code || classified.Retryable != tc.retryable || classified.RetryAfter != tc.delay {
				t.Fatalf("classification id=%q error=%+v", id, err)
			}
			for _, secret := range []string{"private_provider_response", "re_synthetic_reminder_key", "person@example.test", "Frozen text"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error leaked provider/user data: %q", secret)
				}
			}
		})
	}
}

type reminderErrorTransport func(*http.Request) (*http.Response, error)

func (f reminderErrorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestResendReminderNetworkAndCancellationRemainRetryable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  string
	}{
		{"network", errors.New("synthetic network error with private_provider_response"), "provider_network"},
		{"cancelled", context.Canceled, "request_cancelled"},
		{"deadline", context.DeadlineExceeded, "provider_network"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := reminderTestSender("https://provider.invalid")
			sender.client = &http.Client{Transport: reminderErrorTransport(func(*http.Request) (*http.Response, error) { return nil, tc.cause })}
			_, err := sender.SendReminder(context.Background(), reminderProviderFixture())
			var classified *ReminderDeliveryError
			if !errors.As(err, &classified) || classified.Code != tc.want || !classified.Retryable {
				t.Fatalf("error=%v", err)
			}
			if strings.Contains(err.Error(), "private_provider_response") {
				t.Fatal("network error leaked internal details")
			}
		})
	}
}

func TestResendReminderUnavailableAndInvalidPayloadDoNotCallProvider(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.Write([]byte(`{"id":"unexpected"}`)) }))
	defer server.Close()
	unconfigured := NewResendSender(config.ResendConfig{FromEmail: "noreply@timeleak.kz", BaseURL: server.URL}, 5*time.Minute, nil)
	_, err := unconfigured.SendReminder(context.Background(), reminderProviderFixture())
	var classified *ReminderDeliveryError
	if !errors.As(err, &classified) || classified.Code != "sender_unavailable" || classified.Retryable {
		t.Fatalf("unavailable=%v", err)
	}
	for _, mutate := range []func(*domain.ReminderDelivery){func(d *domain.ReminderDelivery) { d.Email = "invalid address" }, func(d *domain.ReminderDelivery) { d.ID = "" }, func(d *domain.ReminderDelivery) { d.Subject = "" }, func(d *domain.ReminderDelivery) { d.Text = "" }, func(d *domain.ReminderDelivery) { d.HTML = "" }} {
		delivery := reminderProviderFixture()
		mutate(&delivery)
		_, err := reminderTestSender(server.URL).SendReminder(context.Background(), delivery)
		if !errors.As(err, &classified) || classified.Code != "invalid_payload" || classified.Retryable {
			t.Fatalf("invalid=%v", err)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid/unconfigured request count=%d", requests)
	}
}

func TestResendReminderRetryAfterHonorsHTTPDatesAndClampsWithoutOverflow(t *testing.T) {
	now := time.Date(2026, 11, 4, 4, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"120", 2 * time.Minute}, {" 3 ", 3 * time.Second}, {"0", 0}, {"-1", 0}, {"invalid", 0},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second}, {now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{strconv.Itoa(math.MaxInt), 20 * time.Hour}, {"72000", 20 * time.Hour}, {now.Add(48 * time.Hour).Format(http.TimeFormat), 20 * time.Hour},
	} {
		if got := resendRetryAfter(tc.header, now); got != tc.want {
			t.Errorf("retry header %q = %v want %v", tc.header, got, tc.want)
		}
	}
}
