package domain

import (
	"errors"
	"strings"
	"time"
)

const (
	ReminderPending     = "pending"
	ReminderSending     = "sending"
	ReminderRetry       = "retry"
	ReminderSent        = "sent"
	ReminderFailed      = "failed"
	ReminderCancelled   = "cancelled"
	MaxReminderAttempts = 8
)

var ErrInvalidReminderSettings = errors.New("reminder settings require days_before 1..30, send_hour 0..23 and a valid IANA timezone")

type ReminderSettings struct {
	Enabled    bool      `json:"enabled"`
	DaysBefore int       `json:"days_before"`
	SendHour   int       `json:"send_hour"`
	Timezone   string    `json:"timezone"`
	Revision   int64     `json:"revision"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func ValidateReminderSettings(settings ReminderSettings) error {
	if settings.DaysBefore < 1 || settings.DaysBefore > 30 || settings.SendHour < 0 || settings.SendHour > 23 || strings.TrimSpace(settings.Timezone) != settings.Timezone || settings.Timezone == "" || settings.Timezone == "Local" {
		return ErrInvalidReminderSettings
	}
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
		return ErrInvalidReminderSettings
	}
	return nil
}

type EmailReminderContact struct {
	UserID     string     `json:"-"`
	Email      string     `json:"email"`
	VerifiedAt *time.Time `json:"verified_at"`
	Enabled    bool       `json:"enabled"`
	Language   string     `json:"language"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type ReminderCandidate struct {
	Note             Note
	Email            string
	Language         string
	ContactUpdatedAt time.Time
}

// Subject, Text and HTML are frozen at enqueue. Retries must use the identical
// payload with the same provider idempotency key, even after a note is edited.
type ReminderDelivery struct {
	ID               string
	UserID           string
	NoteID           string
	OccurrenceDate   string
	DaysBefore       int
	NotificationDate string
	Email            string
	Language         string
	Title            string
	Subject          string
	Text             string
	HTML             string
	Status           string
	Attempts         int
	LeaseToken       string
	LeaseUntil       time.Time
	NextAttemptAt    time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	SentAt           *time.Time
	ProviderID       string
	LastError        string
}

type ReminderStatus struct {
	SenderConfigured bool       `json:"sender_configured"`
	VerifiedContacts int        `json:"verified_contacts"`
	Pending          int        `json:"pending"`
	Sent             int        `json:"sent"`
	Failed           int        `json:"failed"`
	Cancelled        int        `json:"cancelled"`
	LastRunAt        *time.Time `json:"last_run_at"`
	NextRunAt        *time.Time `json:"next_run_at"`
	LastSentAt       *time.Time `json:"last_sent_at"`
}
