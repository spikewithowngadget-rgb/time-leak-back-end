package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	"time-leak/internal/domain"
	"time-leak/internal/service"
)

type reminderAdminFake struct {
	settings domain.ReminderSettings
	status   domain.ReminderStatus
	updates  int
	reads    int
	err      error
}

func (s *reminderAdminFake) GetReminderSettings(context.Context) (domain.ReminderSettings, error) {
	s.reads++
	return s.settings, s.err
}
func (s *reminderAdminFake) UpdateReminderSettings(_ context.Context, value domain.ReminderSettings) (domain.ReminderSettings, error) {
	s.updates++
	if s.err == nil {
		s.settings = value
	}
	return s.settings, s.err
}
func (s *reminderAdminFake) ReminderStatus(context.Context) (domain.ReminderStatus, error) {
	s.reads++
	return s.status, s.err
}

func newReminderAdminTestServer(t *testing.T) (*notesFixture, *reminderAdminFake, *httptest.Server) {
	t.Helper()
	f := newNotesFixture(t)
	fake := &reminderAdminFake{settings: domain.ReminderSettings{Enabled: true, DaysBefore: 3, SendHour: 9, Timezone: "Asia/Almaty"}, status: domain.ReminderStatus{SenderConfigured: true, VerifiedContacts: 4, Pending: 2, Sent: 10}}
	h := &Handler{jwt: service.NewAuthService(f.cfg.JWT, f.repo, zap.NewNop()), reminders: fake}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/reminders/settings", h.AdminGetReminderSettings)
	mux.HandleFunc("PUT /api/v1/admin/reminders/settings", h.AdminUpdateReminderSettings)
	mux.HandleFunc("GET /api/v1/admin/reminders/status", h.AdminReminderStatus)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, fake, srv
}

func TestReminderAdminRequiresAdministrator(t *testing.T) {
	f, fake, srv := newReminderAdminTestServer(t)
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {"invalid", 401}, {f.tokens[0], 403}} {
		for _, route := range []struct{ method, path string }{{"GET", "/api/v1/admin/reminders/settings"}, {"GET", "/api/v1/admin/reminders/status"}, {"PUT", "/api/v1/admin/reminders/settings"}} {
			resp := doReqAuth(t, srv.URL, route.method, route.path, map[string]any{"enabled": true, "days_before": 3, "send_hour": 9, "timezone": "Asia/Almaty"}, tc.token)
			requireNoteStatus(t, resp, tc.status)
			resp.Body.Close()
		}
	}
	if fake.reads != 0 || fake.updates != 0 {
		t.Fatal("unauthorized request reached reminders service")
	}
	for _, path := range []string{"/api/v1/admin/reminders/settings", "/api/v1/admin/reminders/status"} {
		resp := doReqAuth(t, srv.URL, http.MethodGet, path, nil, f.admin)
		requireNoteStatus(t, resp, 200)
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("admin reminder data may be cached")
		}
		resp.Body.Close()
	}
}

func TestReminderAdminValidatesBeforeUpdating(t *testing.T) {
	f, fake, srv := newReminderAdminTestServer(t)
	for _, body := range []string{
		`{}`, `{"enabled":true,"days_before":0,"send_hour":9,"timezone":"Asia/Almaty"}`,
		`{"enabled":true,"days_before":31,"send_hour":9,"timezone":"Asia/Almaty"}`,
		`{"enabled":true,"days_before":3,"send_hour":24,"timezone":"Asia/Almaty"}`,
		`{"enabled":true,"days_before":3,"send_hour":-1,"timezone":"Asia/Almaty"}`,
		`{"enabled":true,"days_before":3,"send_hour":9,"timezone":"Local"}`,
		`{"enabled":true,"days_before":3,"send_hour":9,"timezone":"Missing/Zone"}`,
		`{"enabled":true,"days_before":3,"send_hour":9,"timezone":"Asia/Almaty","send_now":true}`,
		`{"enabled":true,"days_before":3,"send_hour":9,"timezone":"Asia/Almaty"}{}`,
		`{"enabled":"yes","days_before":3,"send_hour":9,"timezone":"Asia/Almaty"}`,
	} {
		request, err := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/admin/reminders/settings", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+f.admin)
		resp, err := srv.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		requireNoteStatus(t, resp, 400)
		resp.Body.Close()
	}
	if fake.updates != 0 {
		t.Fatal("invalid settings reached reminder service")
	}
	for _, days := range []int{1, 3, 7, 14, 30} {
		resp := doReqAuth(t, srv.URL, http.MethodPut, "/api/v1/admin/reminders/settings", map[string]any{"enabled": false, "days_before": days, "send_hour": 0, "timezone": "Asia/Almaty"}, f.admin)
		requireNoteStatus(t, resp, 200)
		var settings domain.ReminderSettings
		decodeJSON(t, resp, &settings)
		if settings.Enabled || settings.DaysBefore != days || settings.SendHour != 0 {
			t.Fatalf("settings not preserved: %+v", settings)
		}
	}
	if fake.updates != 5 {
		t.Fatal("valid settings were not saved")
	}
}

func TestReminderAdminServiceFailureDoesNotExposeDetails(t *testing.T) {
	f, fake, srv := newReminderAdminTestServer(t)
	fake.err = errors.New("private-recipient@example.test provider-secret")
	resp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/admin/reminders/status", nil, f.admin)
	requireNoteStatus(t, resp, 500)
	var body map[string]string
	decodeJSON(t, resp, &body)
	if strings.Contains(body["error"], "example.test") || strings.Contains(body["error"], "provider-secret") {
		t.Fatal("private service detail exposed")
	}
}
