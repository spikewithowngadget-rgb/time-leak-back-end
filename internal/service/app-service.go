package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"time-leak/internal/domain"
	"time-leak/internal/repository"
	dbtraits "time-leak/traits/database"

	"go.uber.org/zap"
)

var (
	ErrPhoneRequired            = errors.New("phone is required")
	ErrInvalidPhoneFormat       = errors.New("phone format is invalid")
	ErrPasswordRequired         = errors.New("password is required")
	ErrPasswordMismatch         = errors.New("passwords do not match")
	ErrPasswordTooShort         = errors.New("password must be at least 8 characters")
	ErrNoteTypeRequired         = errors.New("note_type is required")
	ErrTooManyNoteFiles         = errors.New("too many note files")
	ErrUserAlreadyExists        = errors.New("user already exists")
	ErrUserNotFound             = errors.New("user not found")
	ErrInvalidCredentials       = errors.New("invalid credentials")
	ErrVerificationTokenMissing = errors.New("verification token is required")
	ErrVerificationNotFound     = errors.New("verification token not found")
	ErrVerificationExpired      = errors.New("verification token expired")
	ErrVerificationAlreadyUsed  = errors.New("verification token already used")
	ErrVerificationInvalid      = errors.New("verification token is invalid")
)

type AuthVerificationToken struct {
	VerificationToken string `json:"verification_token"`
	Phone             string `json:"phone"`
	ExpiresInSeconds  int    `json:"expires_in_seconds"`
}

type AppService struct {
	repo            *repository.Repository
	verificationTTL time.Duration
	log             *zap.Logger
}

func NewAppService(repo *repository.Repository, verificationTTL time.Duration, log *zap.Logger) *AppService {
	if log == nil {
		log = zap.NewNop()
	}

	if verificationTTL <= 0 {
		verificationTTL = 5 * time.Minute
	}

	return &AppService{
		repo:            repo,
		verificationTTL: verificationTTL,
		log:             log,
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

func (s *AppService) CreateNote(ctx context.Context, userID, noteType string, noteFiles []string) (domain.Note, error) {
	noteType = strings.TrimSpace(noteType)
	if noteType == "" {
		return domain.Note{}, ErrNoteTypeRequired
	}
	if len(noteFiles) > 5 {
		return domain.Note{}, ErrTooManyNoteFiles
	}

	return s.repo.CreateNote(ctx, userID, noteType, noteFiles)
}

func (s *AppService) GetNote(ctx context.Context, noteID, userID string) (domain.Note, error) {
	return s.repo.GetNoteByIDForUser(ctx, noteID, userID)
}

func (s *AppService) UpdateNote(ctx context.Context, noteID, userID, noteType string, noteFiles []string) (domain.Note, error) {
	noteType = strings.TrimSpace(noteType)
	if noteType == "" {
		return domain.Note{}, ErrNoteTypeRequired
	}
	if len(noteFiles) > 5 {
		return domain.Note{}, ErrTooManyNoteFiles
	}

	return s.repo.UpdateNote(ctx, noteID, userID, noteType, noteFiles)
}

func (s *AppService) DeleteNote(ctx context.Context, noteID, userID string) error {
	return s.repo.DeleteNote(ctx, noteID, userID)
}

func (s *AppService) ListNotes(ctx context.Context, userID string) ([]domain.Note, error) {
	return s.repo.ListNotesByUserID(ctx, userID)
}

func (s *AppService) CreateAuthVerification(
	ctx context.Context,
	purpose domain.AuthVerificationPurpose,
	requestID string,
	phone string,
) (AuthVerificationToken, error) {
	purpose = normalizeAuthVerificationPurpose(purpose)
	if purpose == "" {
		return AuthVerificationToken{}, ErrVerificationInvalid
	}

	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return AuthVerificationToken{}, err
	}

	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return AuthVerificationToken{}, ErrVerificationInvalid
	}

	verificationID := dbtraits.GenerateUUID()
	expiresAt := time.Now().UTC().Add(s.verificationTTL)
	created, err := s.repo.CreateAuthVerification(
		ctx,
		verificationID,
		requestID,
		purpose,
		normalizedPhone,
		expiresAt,
	)
	if err != nil {
		return AuthVerificationToken{}, err
	}

	return AuthVerificationToken{
		VerificationToken: created.ID,
		Phone:             created.Phone,
		ExpiresInSeconds:  int(time.Until(created.ExpiresAt).Seconds()),
	}, nil
}

func (s *AppService) RegisterWithPhoneOTP(
	ctx context.Context,
	phone string,
	password string,
	confirmPassword string,
	verificationToken string,
) (domain.User, error) {
	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return domain.User{}, err
	}

	if err := validatePasswordInput(password, confirmPassword); err != nil {
		return domain.User{}, err
	}

	if err := s.consumeAuthVerification(ctx, verificationToken, domain.AuthVerificationPurposeRegistration, normalizedPhone); err != nil {
		return domain.User{}, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return domain.User{}, err
	}

	created, err := s.repo.CreateUserWithPhone(
		ctx,
		pseudoEmailFromPhone(normalizedPhone),
		normalizedPhone,
		passwordHash,
		"en",
	)
	if err != nil {
		if isUniqueConstraintError(err) {
			return domain.User{}, ErrUserAlreadyExists
		}
		return domain.User{}, fmt.Errorf("create user with phone: %w", err)
	}

	return sanitizeUser(created), nil
}

func (s *AppService) LoginByPhonePassword(ctx context.Context, phone, password string) (domain.User, error) {
	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return domain.User{}, err
	}

	if strings.TrimSpace(password) == "" {
		return domain.User{}, ErrPasswordRequired
	}

	user, err := s.repo.GetUserByPhone(ctx, normalizedPhone)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, fmt.Errorf("get user by phone: %w", err)
	}

	if !comparePasswordHash(user.Password, password) {
		return domain.User{}, ErrInvalidCredentials
	}

	return sanitizeUser(user), nil
}

func (s *AppService) ResetPasswordWithOTP(
	ctx context.Context,
	phone string,
	newPassword string,
	confirmPassword string,
	verificationToken string,
) error {
	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return err
	}

	if err := validatePasswordInput(newPassword, confirmPassword); err != nil {
		return err
	}

	if err := s.consumeAuthVerification(ctx, verificationToken, domain.AuthVerificationPurposePasswordReset, normalizedPhone); err != nil {
		return err
	}

	passwordHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}

	if err := s.repo.UpdateUserPasswordByPhone(ctx, normalizedPhone, passwordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("update user password: %w", err)
	}

	return nil
}

func (s *AppService) consumeAuthVerification(
	ctx context.Context,
	verificationToken string,
	purpose domain.AuthVerificationPurpose,
	phone string,
) error {
	purpose = normalizeAuthVerificationPurpose(purpose)
	verificationToken = strings.TrimSpace(verificationToken)
	if verificationToken == "" {
		return ErrVerificationTokenMissing
	}

	verification, err := s.repo.GetAuthVerificationByID(ctx, verificationToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVerificationNotFound
		}
		return err
	}

	if verification.UsedAt != nil {
		return ErrVerificationAlreadyUsed
	}

	if time.Now().UTC().After(verification.ExpiresAt) {
		return ErrVerificationExpired
	}

	if verification.Purpose != purpose {
		return ErrVerificationInvalid
	}
	if normalizePhone(verification.Phone) != phone {
		return ErrVerificationInvalid
	}

	if err := s.repo.MarkAuthVerificationUsed(ctx, verification.ID, time.Now().UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVerificationAlreadyUsed
		}
		return err
	}

	return nil
}

func normalizeAuthVerificationPurpose(purpose domain.AuthVerificationPurpose) domain.AuthVerificationPurpose {
	switch strings.TrimSpace(strings.ToLower(string(purpose))) {
	case string(domain.AuthVerificationPurposeRegistration):
		return domain.AuthVerificationPurposeRegistration
	case string(domain.AuthVerificationPurposePasswordReset):
		return domain.AuthVerificationPurposePasswordReset
	default:
		return domain.AuthVerificationPurpose("")
	}
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
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
