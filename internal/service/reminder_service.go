package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"go.uber.org/zap"
	"time-leak/internal/domain"
	"time-leak/internal/repository"
)

const reminderPollInterval = time.Minute
const reminderLeaseDuration = 2 * time.Minute
const reminderRetryMaxAge = 20 * time.Hour // Resend keeps idempotency keys for 24h.

type ReminderSender interface {
	Configured() bool
	SendReminder(context.Context, domain.ReminderDelivery) (string, error)
}

type ReminderService struct {
	repo                *repository.Repository
	sender              ReminderSender
	publicBaseURL       string
	log                 *zap.Logger
	tickMu              sync.Mutex
	stateMu             sync.RWMutex
	lastRunAt           *time.Time
	providerPausedUntil time.Time
	sendSpacing         time.Duration
}

func NewReminderService(repo *repository.Repository, sender ReminderSender, publicBaseURL string, log *zap.Logger) *ReminderService {
	if log == nil {
		log = zap.NewNop()
	}
	return &ReminderService{repo: repo, sender: sender, publicBaseURL: strings.TrimRight(publicBaseURL, "/"), log: log, sendSpacing: time.Second}
}

func (s *ReminderService) GetReminderSettings(ctx context.Context) (domain.ReminderSettings, error) {
	return s.repo.GetReminderSettings(ctx)
}
func (s *ReminderService) UpdateReminderSettings(ctx context.Context, settings domain.ReminderSettings) (domain.ReminderSettings, error) {
	if err := domain.ValidateReminderSettings(settings); err != nil {
		return domain.ReminderSettings{}, err
	}
	return s.repo.UpdateReminderSettings(ctx, settings)
}
func (s *ReminderService) ReminderStatus(ctx context.Context) (domain.ReminderStatus, error) {
	status, err := s.repo.GetReminderStatus(ctx)
	if err != nil {
		return status, err
	}
	status.SenderConfigured = s.sender != nil && s.sender.Configured()
	s.stateMu.RLock()
	pausedUntil := s.providerPausedUntil
	if s.lastRunAt != nil {
		last := *s.lastRunAt
		status.LastRunAt = &last
	}
	s.stateMu.RUnlock()
	settings, err := s.repo.GetReminderSettings(ctx)
	if err != nil {
		return status, err
	}
	if settings.Enabled && status.SenderConfigured {
		location, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return status, err
		}
		now := time.Now().In(location)
		next := now.Add(reminderPollInterval)
		if now.Hour() < settings.SendHour {
			next = time.Date(now.Year(), now.Month(), now.Day(), settings.SendHour, 0, 0, 0, location)
		} else if next.Day() != now.Day() {
			next = time.Date(next.Year(), next.Month(), next.Day(), settings.SendHour, 0, 0, 0, location)
		}
		status.NextRunAt = &next
		if pausedUntil.After(next) {
			paused := pausedUntil.In(location)
			status.NextRunAt = &paused
		}
	}
	return status, nil
}

// Run is tied to the API lifecycle. No cron, browser tab, or mobile callback is required.
func (s *ReminderService) Run(ctx context.Context) {
	s.runTick(ctx)
	ticker := time.NewTicker(reminderPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runTick(ctx)
		}
	}
}
func (s *ReminderService) runTick(ctx context.Context) {
	if err := s.Tick(ctx, time.Now()); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Warn("email reminder tick failed", zap.Error(err))
	}
}

// Tick accepts a clock value so date boundaries and restarts can be tested without sleeps.
func (s *ReminderService) Tick(ctx context.Context, now time.Time) error {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	tickStarted := time.Now()
	now = now.UTC()
	s.stateMu.Lock()
	last := now
	s.lastRunAt = &last
	s.stateMu.Unlock()
	settings, err := s.repo.GetReminderSettings(ctx)
	if err != nil {
		return err
	}
	if err := domain.ValidateReminderSettings(settings); err != nil {
		return err
	}
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return err
	}
	local := now.In(location)
	today := local.Format(time.DateOnly)
	if err := s.repo.CancelReminderDeliveriesBefore(ctx, today, now); err != nil {
		return err
	}
	if !settings.Enabled || s.sender == nil || !s.sender.Configured() || local.Hour() < settings.SendHour {
		return nil
	}
	s.stateMu.RLock()
	paused := now.Before(s.providerPausedUntil)
	s.stateMu.RUnlock()
	if paused {
		return nil
	}
	civil := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	from := civil.AddDate(0, 0, 1).Format(time.DateOnly)
	to := civil.AddDate(0, 0, settings.DaysBefore).Format(time.DateOnly)
	candidates, err := s.repo.ListReminderCandidates(ctx, from, to)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validReminderAddress(candidate.Email) {
			continue
		}
		for _, occurrence := range reminderOccurrences(candidate.Note, from, to) {
			eventDate, _ := time.Parse(time.DateOnly, occurrence)
			days := int(eventDate.Sub(civil) / (24 * time.Hour))
			if days < 1 || days > settings.DaysBefore {
				continue
			}
			language := normalizeReminderLanguage(candidate.Language)
			title := reminderTitle(candidate.Note)
			delivery := domain.ReminderDelivery{ID: reminderID(candidate.Note.UserID, candidate.Note.ID, occurrence, days), UserID: candidate.Note.UserID, NoteID: candidate.Note.ID, OccurrenceDate: occurrence, DaysBefore: days, NotificationDate: today, Email: candidate.Email, Language: language, Title: title, Status: domain.ReminderPending}
			message := renderReminderMessage(delivery, s.publicBaseURL)
			delivery.Subject, delivery.Text, delivery.HTML = message.Subject, message.Text, message.HTML
			if _, err := s.repo.EnqueueReminderDelivery(ctx, delivery, now); err != nil {
				return err
			}
		}
	}
	// Bounded batches keep requests and graceful shutdown responsive. A queue larger
	// than one batch is picked up on the next minute, at one provider request/second.
	for count := 0; count < 100; count++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		checkNow := now.Add(time.Since(tickStarted))
		if checkNow.In(location).Format(time.DateOnly) != today {
			return nil
		}
		delivery, err := s.repo.ClaimReminderDelivery(ctx, today, checkNow, checkNow.Add(reminderLeaseDuration))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		freshSettings, err := s.repo.GetReminderSettings(ctx)
		if err != nil {
			return err
		}
		current, validErr := s.repo.GetReminderCandidate(ctx, delivery.UserID, delivery.NoteID)
		invalid := errors.Is(validErr, sql.ErrNoRows)
		if validErr != nil && !invalid {
			return validErr
		}
		freshLocation, locationErr := time.LoadLocation(freshSettings.Timezone)
		if locationErr != nil {
			return locationErr
		}
		if !freshSettings.Enabled || checkNow.In(freshLocation).Hour() < freshSettings.SendHour {
			if err := s.repo.FinishReminderDelivery(ctx, delivery.ID, delivery.LeaseToken, domain.ReminderRetry, "", "schedule_paused", checkNow.Add(time.Minute), checkNow); err != nil {
				return err
			}
			return nil
		}
		if invalid || !reminderStillValid(delivery, current, freshSettings, checkNow) {
			if err := s.repo.FinishReminderDelivery(ctx, delivery.ID, delivery.LeaseToken, domain.ReminderCancelled, "", "eligibility_changed", time.Time{}, checkNow); err != nil {
				return err
			}
			continue
		}
		providerID, sendErr := s.sender.SendReminder(ctx, delivery)
		status := domain.ReminderSent
		errorCode := ""
		nextAttempt := time.Time{}
		if sendErr != nil {
			status = domain.ReminderFailed
			errorCode = "delivery_failed"
			var classified *ReminderDeliveryError
			if errors.As(sendErr, &classified) {
				errorCode = classified.Code
				if classified.Retryable && delivery.Attempts < domain.MaxReminderAttempts {
					delay := time.Minute * time.Duration(1<<min(delivery.Attempts-1, 6))
					if classified.RetryAfter > delay {
						delay = classified.RetryAfter
					}
					nextAttempt = checkNow.Add(delay)
					if nextAttempt.In(location).Format(time.DateOnly) == today && nextAttempt.Sub(delivery.CreatedAt) < reminderRetryMaxAge {
						status = domain.ReminderRetry
					}
				}
			}
			s.log.Warn("email reminder delivery failed", zap.String("delivery_id", delivery.ID), zap.String("code", errorCode), zap.Int("attempt", delivery.Attempts))
		}
		// A provider may have accepted the email just before shutdown. Persist the
		// outcome independently of the cancelled request; the same idempotency key is
		// retained if even this write fails or the process is interrupted.
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		finishErr := s.repo.FinishReminderDelivery(persistCtx, delivery.ID, delivery.LeaseToken, status, providerID, errorCode, nextAttempt, checkNow)
		cancel()
		if finishErr != nil {
			return finishErr
		}
		if sendErr == nil {
			s.log.Info("email reminder accepted", zap.String("delivery_id", delivery.ID))
		}
		var providerErr *ReminderDeliveryError
		if errors.As(sendErr, &providerErr) && (providerErr.Code == "provider_auth" || providerErr.Retryable) {
			delay := max(providerErr.RetryAfter, time.Minute)
			if providerErr.Code == "provider_auth" || providerErr.Code == "provider_quota" {
				delay = max(delay, 30*time.Minute)
			}
			s.stateMu.Lock()
			s.providerPausedUntil = checkNow.Add(delay)
			s.stateMu.Unlock()
			return nil
		}
		if s.sendSpacing > 0 {
			timer := time.NewTimer(s.sendSpacing)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

func reminderID(userID, noteID, occurrence string, days int) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", userID, noteID, occurrence, days))))
}
func reminderOccurrences(note domain.Note, from, to string) []string {
	if note.Lifecycle == domain.NoteRecurring {
		return domain.NoteOccurrences(note, from, to)
	}
	if !note.IsCompleted && domain.ValidNoteDate(note.NoteDate) && note.NoteDate >= from && note.NoteDate <= to {
		return []string{note.NoteDate}
	}
	return nil
}
func validReminderAddress(email string) bool {
	_, valid := domain.NormalizeEmailAddress(email)
	return valid && !strings.HasSuffix(strings.ToLower(email), ".local") && !strings.HasSuffix(strings.ToLower(email), ".invalid")
}
func reminderTitle(note domain.Note) string {
	title := strings.Join(strings.Fields(domain.NormalizeNoteTitle(note.Title)), " ")
	if title == "" {
		return "TimeLeak"
	}
	return title
}
func reminderStillValid(delivery domain.ReminderDelivery, candidate domain.ReminderCandidate, settings domain.ReminderSettings, now time.Time) bool {
	if !validReminderAddress(candidate.Email) || candidate.Email != delivery.Email || reminderTitle(candidate.Note) != delivery.Title || normalizeReminderLanguage(candidate.Language) != delivery.Language || delivery.DaysBefore > settings.DaysBefore || now.Sub(delivery.CreatedAt) >= reminderRetryMaxAge {
		return false
	}
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return false
	}
	if now.In(location).Format(time.DateOnly) != delivery.NotificationDate || now.In(location).Hour() < settings.SendHour {
		return false
	}
	civil, _ := time.Parse(time.DateOnly, delivery.NotificationDate)
	event, _ := time.Parse(time.DateOnly, delivery.OccurrenceDate)
	if int(event.Sub(civil)/(24*time.Hour)) != delivery.DaysBefore {
		return false
	}
	return len(reminderOccurrences(candidate.Note, delivery.OccurrenceDate, delivery.OccurrenceDate)) > 0
}

func reminderCalendarURL(base, date string) string { return base + "/?date=" + url.QueryEscape(date) }
