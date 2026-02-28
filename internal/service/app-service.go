package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"time-leak/internal/domain"
	"time-leak/internal/repository"
	dbtraits "time-leak/traits/database"

	"go.uber.org/zap"
)

var (
	ErrInvalidCredentials       = errors.New("invalid credentials")
	ErrEmailAlreadyExists       = errors.New("email already exists")
	ErrEmailRequiredForWhatsApp = errors.New("email is required for whatsapp otp")
	ErrEmailAlreadyLinked       = errors.New("email already linked to another user")
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

func (s *AppService) RegisterUser(ctx context.Context, email, password, userLanguage string) (domain.User, error) {
	email = normalizeEmail(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return domain.User{}, errors.New("email and password are required")
	}

	user, err := s.repo.CreateUser(ctx, email, hashPassword(password), userLanguage)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.User{}, ErrEmailAlreadyExists
		}
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}

	return sanitizeUser(user), nil
}

func (s *AppService) Login(ctx context.Context, email, password string) (domain.User, error) {
	email = normalizeEmail(email)
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return domain.User{}, ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, fmt.Errorf("get user by email: %w", err)
	}

	if user.Password != hashPassword(password) {
		return domain.User{}, ErrInvalidCredentials
	}

	return sanitizeUser(user), nil
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

func (s *AppService) ResolveUserForOTP(
	ctx context.Context,
	channel domain.OTPChannel,
	destination string,
	email string,
) (domain.User, error) {
	switch channel {
	case domain.OTPChannelEmail:
		return s.resolveUserByEmailOTP(ctx, destination)
	case domain.OTPChannelWhatsApp:
		return s.resolveUserByWhatsAppOTP(ctx, destination, email)
	default:
		return domain.User{}, ErrInvalidOTPChannel
	}
}

func (s *AppService) resolveUserByEmailOTP(ctx context.Context, email string) (domain.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return domain.User{}, errors.New("email is required")
	}

	user, err := s.repo.GetUserByEmail(ctx, email)
	if err == nil {
		return sanitizeUser(user), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("get user by email otp: %w", err)
	}

	created, err := s.repo.CreateUser(ctx, email, dbtraits.GenerateUUID(), "en")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			user, getErr := s.repo.GetUserByEmail(ctx, email)
			if getErr != nil {
				return domain.User{}, getErr
			}
			return sanitizeUser(user), nil
		}
		return domain.User{}, fmt.Errorf("create user by email otp: %w", err)
	}
	return sanitizeUser(created), nil
}

func (s *AppService) resolveUserByWhatsAppOTP(ctx context.Context, phone, email string) (domain.User, error) {
	phone = normalizePhone(phone)
	email = normalizeEmail(email)
	if phone == "" {
		return domain.User{}, errors.New("phone is required")
	}
	if email == "" {
		return domain.User{}, ErrEmailRequiredForWhatsApp
	}

	userByPhone, err := s.repo.GetUserByPhone(ctx, phone)
	if err == nil {
		if userByPhone.Email != email {
			otherByEmail, getErr := s.repo.GetUserByEmail(ctx, email)
			if getErr == nil && otherByEmail.UserID != userByPhone.UserID {
				return domain.User{}, ErrEmailAlreadyLinked
			}
			if getErr != nil && !errors.Is(getErr, sql.ErrNoRows) {
				return domain.User{}, getErr
			}
			if updateErr := s.repo.UpdateUserEmail(ctx, userByPhone.UserID, email); updateErr != nil {
				return domain.User{}, updateErr
			}
		}
		fresh, getErr := s.repo.GetUserByID(ctx, userByPhone.UserID)
		if getErr != nil {
			return domain.User{}, getErr
		}
		return sanitizeUser(fresh), nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("get user by phone: %w", err)
	}

	userByEmail, err := s.repo.GetUserByEmail(ctx, email)
	if err == nil {
		if strings.TrimSpace(userByEmail.Phone) != "" && normalizePhone(userByEmail.Phone) != phone {
			otherByPhone, getErr := s.repo.GetUserByPhone(ctx, phone)
			if getErr == nil && otherByPhone.UserID != userByEmail.UserID {
				return domain.User{}, ErrEmailAlreadyLinked
			}
			if getErr != nil && !errors.Is(getErr, sql.ErrNoRows) {
				return domain.User{}, getErr
			}
		}
		if updateErr := s.repo.UpdateUserPhone(ctx, userByEmail.UserID, phone); updateErr != nil {
			if strings.Contains(strings.ToLower(updateErr.Error()), "unique") {
				return domain.User{}, ErrEmailAlreadyLinked
			}
			return domain.User{}, updateErr
		}
		fresh, getErr := s.repo.GetUserByID(ctx, userByEmail.UserID)
		if getErr != nil {
			return domain.User{}, getErr
		}
		return sanitizeUser(fresh), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("get user by email during whatsapp otp: %w", err)
	}

	created, err := s.repo.CreateUserWithPhone(ctx, email, phone, dbtraits.GenerateUUID(), "en")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.User{}, ErrEmailAlreadyLinked
		}
		return domain.User{}, fmt.Errorf("create user by whatsapp otp: %w", err)
	}
	return sanitizeUser(created), nil
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
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

func sanitizeUser(user domain.User) domain.User {
	user.Password = ""
	return user
}
