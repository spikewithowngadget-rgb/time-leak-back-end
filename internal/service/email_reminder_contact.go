package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"time-leak/internal/domain"
)

var ErrEmailReminderLanguage = errors.New("unsupported email reminder language")

func NormalizeEmailReminderLanguage(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "kz" {
		value = "kk"
	}
	switch value {
	case "", "kk", "ru", "en", "zh", "hi", "fr", "de", "es", "it", "pt", "ar":
		return value, nil
	}
	return "", ErrEmailReminderLanguage
}

var ErrEmailReminderNotVerified = errors.New("verify an email address before enabling email reminders")

func (s *AppService) activeEmailReminderUser(ctx context.Context, userID string) error {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !user.IsActive {
		return ErrUserInactive
	}
	return nil
}

func (s *AppService) GetEmailReminderContact(ctx context.Context, userID string) (domain.EmailReminderContact, error) {
	if err := s.activeEmailReminderUser(ctx, userID); err != nil {
		return domain.EmailReminderContact{}, err
	}
	contact, err := s.repo.GetEmailReminderContact(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.EmailReminderContact{UserID: userID}, nil
	}
	return contact, err
}

func (s *AppService) UpsertVerifiedEmailReminderContact(ctx context.Context, userID, email string, enabled bool, language string) (domain.EmailReminderContact, error) {
	if err := s.activeEmailReminderUser(ctx, userID); err != nil {
		return domain.EmailReminderContact{}, err
	}
	email, valid := domain.NormalizeEmailAddress(email)
	if !valid {
		return domain.EmailReminderContact{}, ErrInvalidOTPDestination
	}
	language, err := NormalizeEmailReminderLanguage(language)
	if err != nil {
		return domain.EmailReminderContact{}, err
	}
	return s.repo.UpsertVerifiedEmailReminderContact(ctx, userID, email, enabled, language)
}

func (s *AppService) SetEmailReminderContactEnabled(ctx context.Context, userID string, enabled bool, language *string) (domain.EmailReminderContact, error) {
	contact, err := s.GetEmailReminderContact(ctx, userID)
	if err != nil {
		return domain.EmailReminderContact{}, err
	}
	if contact.Email == "" || contact.VerifiedAt == nil || contact.VerifiedAt.IsZero() {
		if !enabled {
			return contact, nil
		}
		return domain.EmailReminderContact{}, ErrEmailReminderNotVerified
	}
	if language != nil {
		normalized, err := NormalizeEmailReminderLanguage(*language)
		if err != nil {
			return domain.EmailReminderContact{}, err
		}
		language = &normalized
	}
	return s.repo.SetEmailReminderContactEnabled(ctx, userID, enabled, language)
}
