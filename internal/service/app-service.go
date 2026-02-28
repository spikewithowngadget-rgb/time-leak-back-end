package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"time-leak/internal/domain"
	"time-leak/internal/repository"
	dbtraits "time-leak/traits/database"

	"go.uber.org/zap"
)

var (
	ErrPhoneRequired = errors.New("phone is required")
)

type AppService struct {
	repo *repository.Repository
	log  *zap.Logger
}

func NewAppService(repo *repository.Repository, log *zap.Logger) *AppService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AppService{
		repo: repo,
		log:  log,
	}
}

func (s *AppService) GetUser(ctx context.Context, userID string) (domain.User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	return sanitizeUser(user), nil
}

func (s *AppService) UpdateUserLanguage(ctx context.Context, userID, userLanguage string) error {
	return s.repo.UpdateUserLanguage(ctx, userID, userLanguage)
}

func (s *AppService) CreateNote(ctx context.Context, userID, noteType string) (domain.Note, error) {
	return s.repo.CreateNote(ctx, userID, noteType)
}

func (s *AppService) ListNotes(ctx context.Context, userID string) ([]domain.Note, error) {
	return s.repo.ListNotesByUserID(ctx, userID)
}

func (s *AppService) ResolveUserByPhoneOTP(ctx context.Context, phone string) (domain.User, error) {
	phone = normalizePhone(phone)
	if phone == "" {
		return domain.User{}, ErrPhoneRequired
	}

	userByPhone, err := s.repo.GetUserByPhone(ctx, phone)
	if err == nil {
		return sanitizeUser(userByPhone), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("get user by phone: %w", err)
	}

	created, err := s.repo.CreateUserWithPhone(
		ctx,
		pseudoEmailFromPhone(phone),
		phone,
		dbtraits.GenerateUUID(),
		"en",
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			user, getErr := s.repo.GetUserByPhone(ctx, phone)
			if getErr != nil {
				return domain.User{}, getErr
			}
			return sanitizeUser(user), nil
		}
		return domain.User{}, fmt.Errorf("create user by whatsapp otp: %w", err)
	}
	return sanitizeUser(created), nil
}

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	var b strings.Builder
	for i, r := range phone {
		if i == 0 && r == '+' {
			b.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pseudoEmailFromPhone(phone string) string {
	digits := strings.TrimPrefix(phone, "+")
	if digits == "" {
		digits = "unknown"
	}
	return "wa_" + digits + "@otp.local"
}

func sanitizeUser(user domain.User) domain.User {
	user.Password = ""
	return user
}
