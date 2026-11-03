package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"time-leak/internal/domain"
	"time-leak/traits/database"
)

type reminderRepositoryFixture struct {
	db                    *sql.DB
	repo                  *Repository
	userID, noteID, email string
	now                   time.Time
}

func newReminderRepositoryFixture(t *testing.T) reminderRepositoryFixture {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "reminders.db")+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	if err := database.CreateTables(db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	f := reminderRepositoryFixture{db: db, repo: New(db), userID: uuid.NewString(), noteID: uuid.NewString(), email: "reminders@example.test", now: time.Date(2026, 11, 4, 4, 0, 0, 0, time.UTC)}
	if _, err := db.Exec(`INSERT INTO users(id,email,phone,password,user_language) VALUES(?,?,?,?,?)`, f.userID, "wa_fixture@otp.local", "+77015556677", "preserved-hash", "ru"); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO user_email_reminder_contacts(user_id,email,verified_at,enabled,updated_at) VALUES(?,?,?,1,?)`, f.userID, f.email, f.now.Format(time.RFC3339Nano), f.now.Format(time.RFC3339Nano))
	f.exec(t, `INSERT INTO notes(id,user_id,note_type,note_date,note_date_source,title,lifecycle) VALUES(?,?,'document','2026-11-07','selected','Friend birthday','one_time')`, f.noteID, f.userID)
	return f
}
func (f reminderRepositoryFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
func (f reminderRepositoryFixture) delivery() domain.ReminderDelivery {
	return domain.ReminderDelivery{ID: uuid.NewString(), UserID: f.userID, NoteID: f.noteID, OccurrenceDate: "2026-11-07", DaysBefore: 3, NotificationDate: "2026-11-04", Email: f.email, Language: "ru", Title: "Friend birthday", Subject: "Birthday in 3 days", Text: "Frozen plain text", HTML: "<p>Frozen text</p>"}
}
func (f reminderRepositoryFixture) enqueue(t *testing.T) domain.ReminderDelivery {
	t.Helper()
	delivery := f.delivery()
	added, err := f.repo.EnqueueReminderDelivery(context.Background(), delivery, f.now)
	if err != nil || !added {
		t.Fatalf("enqueue added=%v err=%v", added, err)
	}
	return delivery
}

func TestReminderSettingsDefaultsAndPersistentRevision(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	ctx := context.Background()
	settings, err := f.repo.GetReminderSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || settings.DaysBefore != 3 || settings.SendHour != 9 || settings.Timezone != "Asia/Almaty" || settings.Revision != 1 {
		t.Fatalf("defaults=%+v", settings)
	}
	settings.DaysBefore = 14
	settings.Enabled = false
	settings.SendHour = 10
	updated, err := f.repo.UpdateReminderSettings(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(f.db).GetReminderSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.DaysBefore != 14 || reloaded.Enabled || reloaded.SendHour != 10 || reloaded.Revision != 2 || !reloaded.UpdatedAt.Equal(updated.UpdatedAt) {
		t.Fatalf("settings=%+v", reloaded)
	}
	for _, invalid := range []domain.ReminderSettings{{DaysBefore: 0, SendHour: 9, Timezone: "Asia/Almaty"}, {DaysBefore: 31, SendHour: 9, Timezone: "Asia/Almaty"}, {DaysBefore: 3, SendHour: 24, Timezone: "Asia/Almaty"}, {DaysBefore: 3, SendHour: 9, Timezone: "Invalid/Zone"}} {
		if _, err := f.repo.UpdateReminderSettings(ctx, invalid); !errors.Is(err, domain.ErrInvalidReminderSettings) {
			t.Fatalf("invalid settings=%+v err=%v", invalid, err)
		}
	}
}

func TestReminderCandidatesRespectConsentOwnershipAndLanguage(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	ctx := context.Background()
	list, err := f.repo.ListReminderCandidates(ctx, "2026-11-05", "2026-11-07")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list[0].Email != f.email || list[0].Language != "ru" || list[0].Note.ID != f.noteID || list[0].Note.NoteFiles != nil {
		t.Fatalf("candidate=%+v", list[0])
	}
	f.exec(t, `UPDATE user_email_reminder_contacts SET language='kk' WHERE user_id=?`, f.userID)
	candidate, err := f.repo.GetReminderCandidate(ctx, f.userID, f.noteID)
	if err != nil || candidate.Language != "kk" {
		t.Fatalf("language=%s err=%v", candidate.Language, err)
	}
	if _, err := f.repo.GetReminderCandidate(ctx, uuid.NewString(), f.noteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("wrongowner err=%v", err)
	}
	f.exec(t, `UPDATE user_email_reminder_contacts SET email='new@example.test', enabled=0 WHERE user_id=?`, f.userID)
	if _, err := f.repo.GetReminderCandidate(ctx, f.userID, f.noteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("optout err=%v", err)
	}
	if added, err := f.repo.EnqueueReminderDelivery(ctx, f.delivery(), f.now); err != nil || added {
		t.Fatalf("enqueue afteroptout=%v err=%v", added, err)
	}
	f.exec(t, `UPDATE user_email_reminder_contacts SET enabled=1 WHERE user_id=?`, f.userID)
	candidate, err = f.repo.GetReminderCandidate(ctx, f.userID, f.noteID)
	if err != nil || candidate.Email != "new@example.test" {
		t.Fatalf("changedmail=%+v err=%v", candidate, err)
	}
	if added, err := f.repo.EnqueueReminderDelivery(ctx, f.delivery(), f.now); err != nil || added {
		t.Fatalf("enqueue stalemail=%v err=%v", added, err)
	}
	f.exec(t, `UPDATE users SET is_active=0 WHERE id=?`, f.userID)
	if _, err := f.repo.GetReminderCandidate(ctx, f.userID, f.noteID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("inactive err=%v", err)
	}
}

func TestReminderCandidatesIncludePriorRecurringNotesButNotCompletedOneTime(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	ctx := context.Background()
	f.exec(t, `UPDATE notes SET is_completed=1 WHERE id=?`, f.noteID)
	recurringID := uuid.NewString()
	f.exec(t, `INSERT INTO notes(id,user_id,note_type,note_date,note_date_source,title,lifecycle,recurrence_rule) VALUES(?,?,'image','2020-11-07','selected','Yearly birthday','recurring','FREQ=YEARLY')`, recurringID, f.userID)
	list, err := f.repo.ListReminderCandidates(ctx, "2026-11-05", "2026-11-07")
	if err != nil || len(list) != 1 || list[0].Note.ID != recurringID {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if occurrences := domain.NoteOccurrences(list[0].Note, "2026-11-05", "2026-11-07"); len(occurrences) != 1 || occurrences[0] != "2026-11-07" {
		t.Fatalf("occurrences=%v", occurrences)
	}
}

func TestReminderEnqueueIsDurableDeduplicatedAndPayloadImmutable(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	ctx := context.Background()
	delivery := f.enqueue(t)
	changed := delivery
	changed.ID = uuid.NewString()
	changed.Title = "Edited title"
	changed.Subject = "Changed subject"
	changed.HTML = "<p>New body</p>"
	added, err := New(f.db).EnqueueReminderDelivery(ctx, changed, f.now.Add(time.Minute))
	if err != nil || added {
		t.Fatalf("reenqueue=%v err=%v", added, err)
	}
	claimed, err := New(f.db).ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != delivery.ID || claimed.Subject != delivery.Subject || claimed.HTML != delivery.HTML || claimed.Title != delivery.Title || claimed.LeaseToken == "" || claimed.Attempts != 1 {
		t.Fatalf("claimed=%+v", claimed)
	}
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now.Add(time.Minute), f.now.Add(3*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("activelease err=%v", err)
	}
	if err := database.CreateTables(f.db, zap.NewNop()); err != nil {
		t.Fatal(err)
	}
	var password string
	if err := f.db.QueryRow(`SELECT password FROM users WHERE id=?`, f.userID).Scan(&password); err != nil || password != "preserved-hash" {
		t.Fatalf("migration damaged user: %q err=%v", password, err)
	}
}

func TestReminderClaimConcurrentSingleWinner(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	delivery := f.enqueue(t)
	ctx := context.Background()
	const workers = 12
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := New(f.db).ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute))
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("claim err=%v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("claim winners=%d", wins)
	}
	var attempts int
	f.db.QueryRow(`SELECT attempts FROM email_reminder_deliveries WHERE id=?`, delivery.ID).Scan(&attempts)
	if attempts != 1 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestReminderExpiredLeaseRecoveryAndFencedAcknowledgement(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	delivery := f.enqueue(t)
	ctx := context.Background()
	first, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	later := f.now.Add(3 * time.Minute)
	second, err := New(f.db).ClaimReminderDelivery(ctx, delivery.NotificationDate, later, later.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.LeaseToken == first.LeaseToken || second.Attempts != 2 || second.Text != first.Text {
		t.Fatalf("recovery=%+v", second)
	}
	if err := f.repo.FinishReminderDelivery(ctx, first.ID, first.LeaseToken, domain.ReminderSent, "old-provider", "", time.Time{}, later); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("staleack err=%v", err)
	}
	retryAt := later.Add(10 * time.Minute)
	if err := f.repo.FinishReminderDelivery(ctx, second.ID, second.LeaseToken, domain.ReminderRetry, "", "provider_timeout", retryAt, later); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, later, later.Add(2*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("earlyretry err=%v", err)
	}
	third, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, retryAt, retryAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if third.Attempts != 3 || third.ID != delivery.ID || third.Subject != delivery.Subject {
		t.Fatalf("retry=%+v", third)
	}
	if err := f.repo.FinishReminderDelivery(ctx, third.ID, third.LeaseToken, domain.ReminderSent, "provider-ok", "", time.Time{}, retryAt); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, retryAt, retryAt.Add(2*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("sentclaimed err=%v", err)
	}
	status, err := f.repo.GetReminderStatus(ctx)
	if err != nil || status.Sent != 1 || status.Pending != 0 || status.LastSentAt == nil || !status.LastSentAt.Equal(retryAt) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestReminderClaimsStopWhenDisabledOrOutsideRetryWindow(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	delivery := f.enqueue(t)
	ctx := context.Background()
	settings, _ := f.repo.GetReminderSettings(ctx)
	settings.Enabled = false
	if _, err := f.repo.UpdateReminderSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disabledclaim err=%v", err)
	}
	settings.Enabled = true
	f.repo.UpdateReminderSettings(ctx, settings)
	f.exec(t, `UPDATE email_reminder_deliveries SET attempts=8 WHERE id=?`, delivery.ID)
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("exhaustedclaim err=%v", err)
	}
	status, err := f.repo.GetReminderStatus(ctx)
	if err != nil || status.Failed != 1 || status.Pending != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	f.exec(t, `DELETE FROM email_reminder_deliveries`)
	f.enqueue(t)
	old := f.now.Add(-24 * time.Hour).UnixMilli()
	f.exec(t, `UPDATE email_reminder_deliveries SET created_at=?,attempts=1`, old)
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("oldclaim err=%v", err)
	}
}

func TestReminderCancelsOldDayWithoutBurstCatchupAndCascadesDeletedNote(t *testing.T) {
	f := newReminderRepositoryFixture(t)
	delivery := f.enqueue(t)
	ctx := context.Background()
	if err := f.repo.CancelReminderDeliveriesBefore(ctx, "2026-11-05", f.now.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	status, err := f.repo.GetReminderStatus(ctx)
	if err != nil || status.Cancelled != 1 || status.Pending != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := f.repo.ClaimReminderDelivery(ctx, delivery.NotificationDate, f.now, f.now.Add(2*time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cancelledclaim err=%v", err)
	}
	f.exec(t, `DELETE FROM notes WHERE id=?`, f.noteID)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM email_reminder_deliveries`).Scan(&count)
	if count != 0 {
		t.Fatalf("orphanjobs=%d", count)
	}
}
