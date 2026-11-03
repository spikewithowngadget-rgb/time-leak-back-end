package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"time-leak/internal/domain"
	"time-leak/internal/repository"
)

func emailReminderContactFixture(t *testing.T) (*emailOTPFixture, [2]domain.User, [2]string) {
	t.Helper()
	f := newEmailOTPFixture(t, nil)
	var users [2]domain.User
	var tokens [2]string
	for i := range users {
		user, err := repository.New(f.db).CreateUserWithPhone(context.Background(), fmt.Sprintf("wa_reminder_%d@otp.local", i), fmt.Sprintf("+7701555000%d", i), "unusable-random-password-hash", "en")
		if err != nil {
			t.Fatal(err)
		}
		pair, err := f.svcs.JWT.IssueUserTokens(context.Background(), user, "otp_whatsapp")
		if err != nil {
			t.Fatal(err)
		}
		users[i], tokens[i] = user, pair.AccessToken
	}
	return f, users, tokens
}

func reminderContactRequest(t *testing.T, f *emailOTPFixture, method, path string, body any, token string, status int) domain.EmailReminderContact {
	t.Helper()
	resp := doReqAuth(t, f.srv.URL, method, path, body, token)
	requireNoteStatus(t, resp, status)
	if status == 200 && resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("contact endpoint must not cache private email")
	}
	var contact domain.EmailReminderContact
	decodeJSON(t, resp, &contact)
	return contact
}

func TestEmailReminderContactVerificationConsentAndIsolation(t *testing.T) {
	f, users, tokens := emailReminderContactFixture(t)
	path := "/api/v1/auth/email-reminders"
	reminderContactRequest(t, f, http.MethodGet, path, nil, "", 401)
	empty := reminderContactRequest(t, f, http.MethodGet, path, nil, tokens[0], 200)
	if empty.Email != "" || empty.Enabled || empty.VerifiedAt != nil {
		t.Fatalf("unverified contact exposed: %+v", empty)
	}
	reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": true}, tokens[0], 400)
	reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": false}, tokens[0], 200)
	req := f.request(t, " Reminder.Owner@example.com ")
	code := f.code(req.Email)
	wrong := "0000"
	if wrong == code {
		wrong = "1111"
	}
	reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": req.RequestID, "code": wrong, "enabled": true, "language": "kk"}, tokens[0], 400)
	// Caller-supplied destination/owner fields must never override the OTP's
	// verified mailbox or the authenticated user.
	reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": req.RequestID, "code": code, "email": "substitute@example.com", "enabled": true}, tokens[0], 400)
	reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": req.RequestID, "code": code, "language": "unsupported", "enabled": true}, tokens[0], 400)
	verified := reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": req.RequestID, "code": code, "enabled": true, "language": "kz"}, tokens[0], 200)
	if verified.Email != req.Email || verified.VerifiedAt == nil || !verified.Enabled || verified.Language != "kk" {
		t.Fatalf("wrong verified contact: %+v", verified)
	}
	reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": req.RequestID, "code": code, "enabled": true}, tokens[1], 400)
	other := reminderContactRequest(t, f, http.MethodGet, path, nil, tokens[1], 200)
	if other.Email != "" || other.Enabled {
		t.Fatal("another user saw reminder mailbox")
	}
	stored, err := repository.New(f.db).GetUserByID(context.Background(), users[0].UserID)
	if err != nil || stored.Email != users[0].Email || stored.Phone != users[0].Phone || stored.UserLanguage != users[0].UserLanguage {
		t.Fatalf("reminder contact changed mobile identity: %+v %v", stored, err)
	}
	disabled := reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": false, "language": "ru"}, tokens[0], 200)
	if disabled.Enabled || disabled.Email != req.Email || disabled.Language != "ru" {
		t.Fatalf("opt-out failed: %+v", disabled)
	}
	reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": true, "language": "unknown"}, tokens[0], 400)
	reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": true, "user_id": users[1].UserID}, tokens[0], 400)
	reenabled := reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": true}, tokens[0], 200)
	if !reenabled.Enabled || reenabled.Language != "ru" {
		t.Fatal("re-enable lost verified mailbox/language")
	}
}

func TestEmailReminderContactDefaultsOffAndRejectsInactive(t *testing.T) {
	f, users, tokens := emailReminderContactFixture(t)
	path := "/api/v1/auth/email-reminders"
	req := f.request(t, "consent@example.com")
	verified := reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": req.RequestID, "code": f.code(req.Email), "language": "en"}, tokens[0], 200)
	if verified.Enabled || verified.VerifiedAt == nil {
		t.Fatal("omitted consent opted user into email")
	}
	if err := f.svcs.App.DeactivateUser(context.Background(), users[0].UserID); err != nil {
		t.Fatal(err)
	}
	reminderContactRequest(t, f, http.MethodGet, path, nil, tokens[0], 403)
	reminderContactRequest(t, f, http.MethodPut, path, map[string]any{"enabled": true}, tokens[0], 403)
	newReq := f.request(t, "inactive-reminder@example.com")
	reminderContactRequest(t, f, http.MethodPost, path+"/verify", map[string]any{"request_id": newReq.RequestID, "code": f.code(newReq.Email), "enabled": true}, tokens[0], 403)
}

func TestEmailReminderContactRejectsExpiredAndOldVerifyCodes(t *testing.T) {
	f, _, tokens := emailReminderContactFixture(t)
	path := "/api/v1/auth/email-reminders/verify"
	expired := f.request(t, "expired-reminder@example.com")
	if _, err := f.db.Exec(`UPDATE otp_requests SET expires_at=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), expired.RequestID); err != nil {
		t.Fatal(err)
	}
	reminderContactRequest(t, f, http.MethodPost, path, map[string]any{"request_id": expired.RequestID, "code": f.code(expired.Email), "enabled": true}, tokens[0], 400)
	old := f.request(t, "verified-elsewhere@example.com")
	resp := f.verify(t, old.RequestID, f.code(old.Email), 200)
	resp.Body.Close()
	reminderContactRequest(t, f, http.MethodPost, path, map[string]any{"request_id": old.RequestID, "code": f.code(old.Email), "enabled": true}, tokens[0], 400)
}
