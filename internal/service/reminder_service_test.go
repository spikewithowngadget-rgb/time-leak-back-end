package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"time-leak/internal/domain"
	"time-leak/internal/repository"
	"time-leak/traits/database"
)

type fakeReminderSender struct {
	mu         sync.Mutex
	deliveries []domain.ReminderDelivery
	configured bool
	send       func(domain.ReminderDelivery) (string, error)
}

func (f *fakeReminderSender) Configured() bool { return f.configured }
func (f *fakeReminderSender) SendReminder(_ context.Context, d domain.ReminderDelivery) (string, error) {
	f.mu.Lock()
	f.deliveries = append(f.deliveries, d)
	f.mu.Unlock()
	if f.send != nil {
		return f.send(d)
	}
	return "provider-" + d.ID, nil
}
func (f *fakeReminderSender) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.deliveries) }

type reminderServiceFixture struct {
	db          *sql.DB
	repo        *repository.Repository
	worker      *ReminderService
	sender      *fakeReminderSender
	uid, noteID string
	now         time.Time
}

func newReminderServiceFixture(t *testing.T) reminderServiceFixture {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "service.db")+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	if err := database.CreateTables(db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	f := reminderServiceFixture{db: db, repo: repository.New(db), uid: uuid.NewString(), noteID: uuid.NewString(), now: time.Date(2026, 11, 4, 4, 0, 0, 0, time.UTC), sender: &fakeReminderSender{configured: true}}
	f.exec(t, `INSERT INTO users(id,email,password,phone,user_language) VALUES(?,?,'hash','+77015556677','ru')`, f.uid, "wa_fixture@otp.local")
	f.exec(t, `INSERT INTO user_email_reminder_contacts(user_id,email,verified_at,enabled,language,updated_at) VALUES(?,'verified@example.com',?,1,'ru',?)`, f.uid, f.now.Format(time.RFC3339Nano), f.now.Format(time.RFC3339Nano))
	f.exec(t, `INSERT INTO notes(id,user_id,note_type,note_date,note_date_source,title,lifecycle) VALUES(?,?,'document','2026-11-07','selected','День рождения друга','one_time')`, f.noteID, f.uid)
	f.worker = NewReminderService(f.repo, f.sender, "https://timeleak.kz", zap.NewNop())
	f.worker.sendSpacing = 0
	return f
}
func (f reminderServiceFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func (f reminderServiceFixture) tick(t *testing.T, now time.Time) {
	t.Helper()
	if err := f.worker.Tick(context.Background(), now); err != nil {
		t.Fatal(err)
	}
}
func TestReminderWorkerDefaultThreeTwoOneAcrossRestarts(t *testing.T) {
	f := newReminderServiceFixture(t)
	f.tick(t, f.now.Add(-time.Minute))
	if f.sender.count() != 0 {
		t.Fatal("sent before09Almaty")
	}
	for day := 0; day < 5; day++ {
		now := f.now.AddDate(0, 0, day)
		f.tick(t, now)
		f.tick(t, now.Add(time.Minute))
		restarted := NewReminderService(f.repo, f.sender, "https://timeleak.kz", zap.NewNop())
		restarted.sendSpacing = 0
		if err := restarted.Tick(context.Background(), now.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if f.sender.count() != 3 {
		t.Fatalf("got%d deliveries,need3", f.sender.count())
	}
	for i, d := range f.sender.deliveries {
		if d.DaysBefore != 3-i || d.OccurrenceDate != "2026-11-07" || d.NotificationDate != f.now.AddDate(0, 0, i).In(mustReminderLocation(t, "Asia/Almaty")).Format(time.DateOnly) {
			t.Fatalf("unexpected%d:%+v", i, d)
		}
		if !strings.Contains(d.Text, "День рождения друга") || !strings.Contains(d.Text, "/?date=2026-11-07") {
			t.Fatal("missingevent/deeplink")
		}
	}
}
func mustReminderLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
func TestReminderWorkerRecurringClampingAndUTCDateBoundary(t *testing.T) {
	f := newReminderServiceFixture(t)
	f.exec(t, `UPDATE notes SET lifecycle='recurring',recurrence_rule='FREQ=MONTHLY',note_date='2025-01-31' WHERE id=?`, f.noteID)
	now := time.Date(2026, 2, 24, 23, 59, 0, 0, time.UTC) // Feb25 04:59Almaty; before09.
	f.tick(t, now)
	if f.sender.count() != 0 {
		t.Fatal("early send")
	}
	f.tick(t, time.Date(2026, 2, 25, 4, 0, 0, 0, time.UTC))
	if f.sender.count() != 1 || f.sender.deliveries[0].OccurrenceDate != "2026-02-28" || f.sender.deliveries[0].DaysBefore != 3 {
		t.Fatalf("recurrence=%+v", f.sender.deliveries)
	}
}
func TestReminderWorkerConsentCompletionAndInactiveAccounts(t *testing.T) {
	for _, update := range []string{`UPDATE user_email_reminder_contacts SET enabled=0`, `UPDATE users SET is_active=0`, `UPDATE notes SET is_completed=1`, `DELETE FROM user_email_reminder_contacts`, `UPDATE user_email_reminder_contacts SET email='wa_example@otp.local'`} {
		t.Run(update, func(t *testing.T) {
			f := newReminderServiceFixture(t)
			f.exec(t, update)
			f.tick(t, f.now)
			if f.sender.count() != 0 {
				t.Fatal("ineligiblemailwassent")
			}
		})
	}
	f := newReminderServiceFixture(t)
	f.sender.configured = false
	f.tick(t, f.now)
	if f.sender.count() != 0 {
		t.Fatal("unconfiguredsend")
	}
	settings, _ := f.repo.GetReminderSettings(context.Background())
	settings.Enabled = false
	_, _ = f.repo.UpdateReminderSettings(context.Background(), settings)
	f.sender.configured = true
	f.tick(t, f.now)
	if f.sender.count() != 0 {
		t.Fatal("disabledsend")
	}
}
func TestReminderWorkerRetryFrozenPayloadAndNoCatchupSpam(t *testing.T) {
	f := newReminderServiceFixture(t)
	attempt := 0
	f.sender.send = func(d domain.ReminderDelivery) (string, error) {
		attempt++
		if attempt == 1 {
			return "", &ReminderDeliveryError{Code: "provider_network", Retryable: true}
		}
		return "same-provider-id", nil
	}
	f.tick(t, f.now)
	f.tick(t, f.now.Add(30*time.Second))
	if f.sender.count() != 1 {
		t.Fatal("retriedbeforebackoff")
	}
	f.tick(t, f.now.Add(2*time.Minute))
	if f.sender.count() != 2 {
		t.Fatalf("count=%d", f.sender.count())
	}
	a, b := f.sender.deliveries[0], f.sender.deliveries[1]
	if a.ID != b.ID || a.Subject != b.Subject || a.Text != b.Text || a.HTML != b.HTML {
		t.Fatal("idempotency payloadchanged")
	}
	f.tick(t, f.now.Add(3*time.Minute))
	if f.sender.count() != 2 {
		t.Fatal("sentagainsamecivil-day")
	}
	// A delivery whose result was lost is abandoned after its original notification
	// day, rather than arriving late alongside the next countdown message.
	f2 := newReminderServiceFixture(t)
	f2.sender.send = func(domain.ReminderDelivery) (string, error) {
		return "", &ReminderDeliveryError{Code: "provider_network", Retryable: true}
	}
	f2.tick(t, f2.now)
	f2.sender.send = nil
	f2.tick(t, f2.now.AddDate(0, 0, 1))
	if f2.sender.count() != 2 || f2.sender.deliveries[1].DaysBefore != 2 {
		t.Fatal("old-daycatchup")
	}
}
func TestReminderWorkerRevalidatesEditedNotesAndMailBeforeRetry(t *testing.T) {
	for _, update := range []string{`UPDATE notes SET note_date='2027-01-01'`, `UPDATE notes SET title='Changed title'`, `UPDATE user_email_reminder_contacts SET email='new@example.com'`, `UPDATE user_email_reminder_contacts SET enabled=0`, `DELETE FROM notes`} {
		t.Run(update, func(t *testing.T) {
			f := newReminderServiceFixture(t)
			f.sender.send = func(domain.ReminderDelivery) (string, error) {
				return "", &ReminderDeliveryError{Code: "provider_network", Retryable: true}
			}
			f.tick(t, f.now)
			f.exec(t, update)
			f.sender.send = nil
			f.tick(t, f.now.Add(2*time.Minute))
			if f.sender.count() != 1 {
				t.Fatal("staledeliverysent")
			}
		})
	}
}
func TestReminderWorkerBenignSettingsSaveDoesNotCancelOtherEvents(t *testing.T) {
	f := newReminderServiceFixture(t)
	second := uuid.NewString()
	f.exec(t, `INSERT INTO notes(id,user_id,note_type,note_date,title,lifecycle) VALUES(?,?,'document','2026-11-06','Second event','one_time')`, second, f.uid)
	f.sender.send = func(domain.ReminderDelivery) (string, error) {
		settings, err := f.repo.GetReminderSettings(context.Background())
		if err != nil {
			return "", err
		}
		_, err = f.repo.UpdateReminderSettings(context.Background(), settings)
		return "id", err
	}
	f.tick(t, f.now)
	if f.sender.count() != 2 {
		t.Fatalf("benignsettingupdatelostevent:count%d", f.sender.count())
	}
}
func TestReminderWorkerMultipleInstancesDoNotSendDuplicates(t *testing.T) {
	f := newReminderServiceFixture(t)
	other := NewReminderService(f.repo, f.sender, "https://timeleak.kz", zap.NewNop())
	other.sendSpacing = 0
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, worker := range []*ReminderService{f.worker, other} {
		wg.Add(1)
		go func(s *ReminderService) { defer wg.Done(); errs <- s.Tick(context.Background(), f.now) }(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.sender.count() != 1 {
		t.Fatalf("duplicates:%d", f.sender.count())
	}
}
func TestReminderWorkerLocalHourAndCustomWindow(t *testing.T) {
	f := newReminderServiceFixture(t)
	settings, _ := f.repo.GetReminderSettings(context.Background())
	settings.DaysBefore = 14
	settings.SendHour = 12
	settings.Timezone = "UTC"
	if _, err := f.worker.UpdateReminderSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE notes SET note_date='2026-11-18'`)
	f.tick(t, time.Date(2026, 11, 4, 11, 59, 0, 0, time.UTC))
	if f.sender.count() != 0 {
		t.Fatal("sentbeforeconfiguredhour")
	}
	f.tick(t, time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC))
	if f.sender.count() != 1 || f.sender.deliveries[0].DaysBefore != 14 {
		t.Fatal("custom14daywindowignored")
	}
}
func TestReminderWorkerProviderQuotaPausesOtherJobs(t *testing.T) {
	f := newReminderServiceFixture(t)
	f.exec(t, `INSERT INTO notes(id,user_id,note_type,note_date,title,lifecycle) VALUES(?,?,'document','2026-11-06','Another','one_time')`, uuid.NewString(), f.uid)
	f.sender.send = func(domain.ReminderDelivery) (string, error) {
		return "", &ReminderDeliveryError{Code: "provider_quota", Retryable: true, RetryAfter: time.Hour}
	}
	f.tick(t, f.now)
	f.tick(t, f.now.Add(time.Minute))
	if f.sender.count() != 1 {
		t.Fatal("providerquota ignored")
	}
}
func TestReminderWorkerPermanentProviderErrorAndStatus(t *testing.T) {
	f := newReminderServiceFixture(t)
	f.sender.send = func(domain.ReminderDelivery) (string, error) {
		return "", &ReminderDeliveryError{Code: "provider_rejected_422"}
	}
	f.tick(t, f.now)
	f.tick(t, f.now.Add(time.Hour))
	if f.sender.count() != 1 {
		t.Fatal("permanentrejection retried")
	}
	status, err := f.worker.ReminderStatus(context.Background())
	if err != nil || status.Failed != 1 || status.VerifiedContacts != 1 || status.LastRunAt == nil || !status.SenderConfigured {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.worker.Tick(ctx, f.now); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
