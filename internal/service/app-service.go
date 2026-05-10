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
	ErrUserInactive             = errors.New("user account is deactivated")
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

func (s *AppService) GetUserByPhone(ctx context.Context, phone string) (domain.User, error) {
	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return domain.User{}, err
	}

	user, err := s.repo.GetUserByPhone(ctx, normalizedPhone)
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
	device *AuthDeviceInput,
	location *AuthLocationInput,
	reqCtx AuthRequestContext,
) (domain.User, error) {
	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return domain.User{}, err
	}

	if err := validatePasswordInput(password, confirmPassword); err != nil {
		return domain.User{}, err
	}

	verification, err := s.consumeAuthVerification(ctx, verificationToken, domain.AuthVerificationPurposeRegistration, normalizedPhone)
	if err != nil {
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

	_ = s.persistAuthContext(ctx, created.UserID, created.Phone, verification.RequestID, "register_success", device, location, reqCtx)

	return sanitizeUser(created), nil
}

func (s *AppService) LoginByPhonePassword(
	ctx context.Context,
	phone string,
	password string,
	verificationToken string,
	device *AuthDeviceInput,
	location *AuthLocationInput,
	reqCtx AuthRequestContext,
) (domain.User, error) {
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
			_ = s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
				Phone:       normalizedPhone,
				EventType:   "login_failed",
				IPAddress:   reqCtx.IPAddress,
				UserAgent:   reqCtx.UserAgent,
				MetadataJSON: marshalJSON(map[string]any{"reason": "user_not_found"}),
			})
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, fmt.Errorf("get user by phone: %w", err)
	}

	var verificationRequestID string
	if strings.TrimSpace(verificationToken) != "" {
		verification, verifyErr := s.consumeAuthVerification(ctx, verificationToken, domain.AuthVerificationPurposeLogin, normalizedPhone)
		if verifyErr != nil {
			return domain.User{}, verifyErr
		}
		verificationRequestID = verification.RequestID
	}

	if !user.IsActive {
		_ = s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
			UserID:       user.UserID,
			Phone:        normalizedPhone,
			EventType:    "login_failed",
			IPAddress:    reqCtx.IPAddress,
			UserAgent:    reqCtx.UserAgent,
			MetadataJSON: marshalJSON(map[string]any{"reason": "user_inactive"}),
		})
		return domain.User{}, ErrUserInactive
	}

	if !comparePasswordHash(user.Password, password) {
		_ = s.repo.CreateAuthEvent(ctx, domain.AuthEvent{
			UserID:       user.UserID,
			Phone:        normalizedPhone,
			EventType:    "login_failed",
			IPAddress:    reqCtx.IPAddress,
			UserAgent:    reqCtx.UserAgent,
			MetadataJSON: marshalJSON(map[string]any{"reason": "invalid_password"}),
		})
		return domain.User{}, ErrInvalidCredentials
	}

	_ = s.persistAuthContext(ctx, user.UserID, user.Phone, verificationRequestID, "login_success", device, location, reqCtx)

	return sanitizeUser(user), nil
}

func (s *AppService) DeactivateUser(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ErrUserNotFound
	}

	if err := s.repo.DeactivateUser(ctx, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("deactivate user: %w", err)
	}
	if err := s.repo.RevokeAllUserRefreshTokens(ctx, userID); err != nil {
		return err
	}
	return nil
}

func (s *AppService) ResetPasswordWithOTP(
	ctx context.Context,
	phone string,
	newPassword string,
	confirmPassword string,
	verificationToken string,
	device *AuthDeviceInput,
	location *AuthLocationInput,
	reqCtx AuthRequestContext,
) error {
	normalizedPhone, err := validatePhoneStrict(phone)
	if err != nil {
		return err
	}

	if err := validatePasswordInput(newPassword, confirmPassword); err != nil {
		return err
	}

	verification, err := s.consumeAuthVerification(ctx, verificationToken, domain.AuthVerificationPurposePasswordReset, normalizedPhone)
	if err != nil {
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

	user, err := s.repo.GetUserByPhone(ctx, normalizedPhone)
	if err == nil {
		_ = s.persistAuthContext(ctx, user.UserID, normalizedPhone, verification.RequestID, "password_reset_success", device, location, reqCtx)
	}

	return nil
}

func (s *AppService) consumeAuthVerification(
	ctx context.Context,
	verificationToken string,
	purpose domain.AuthVerificationPurpose,
	phone string,
) (domain.AuthVerification, error) {
	purpose = normalizeAuthVerificationPurpose(purpose)
	verificationToken = strings.TrimSpace(verificationToken)
	if verificationToken == "" {
		return domain.AuthVerification{}, ErrVerificationTokenMissing
	}

	verification, err := s.repo.GetAuthVerificationByID(ctx, verificationToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuthVerification{}, ErrVerificationNotFound
		}
		return domain.AuthVerification{}, err
	}

	if verification.UsedAt != nil {
		return domain.AuthVerification{}, ErrVerificationAlreadyUsed
	}

	if time.Now().UTC().After(verification.ExpiresAt) {
		return domain.AuthVerification{}, ErrVerificationExpired
	}

	if verification.Purpose != purpose {
		return domain.AuthVerification{}, ErrVerificationInvalid
	}
	if normalizePhone(verification.Phone) != phone {
		return domain.AuthVerification{}, ErrVerificationInvalid
	}

	if err := s.repo.MarkAuthVerificationUsed(ctx, verification.ID, time.Now().UTC()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuthVerification{}, ErrVerificationAlreadyUsed
		}
		return domain.AuthVerification{}, err
	}

	return verification, nil
}

func normalizeAuthVerificationPurpose(purpose domain.AuthVerificationPurpose) domain.AuthVerificationPurpose {
	switch strings.TrimSpace(strings.ToLower(string(purpose))) {
	case string(domain.AuthVerificationPurposeRegistration):
		return domain.AuthVerificationPurposeRegistration
	case string(domain.AuthVerificationPurposeLogin):
		return domain.AuthVerificationPurposeLogin
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

func (s *AppService) ListUserDevices(ctx context.Context, userID string) ([]domain.UserDevice, error) {
	return s.repo.ListUserDevicesByUserID(ctx, userID)
}

func (s *AppService) DeactivateUserDevice(ctx context.Context, userID, deviceID string) error {
	return s.repo.DeactivateUserDevice(ctx, userID, deviceID)
}

func (s *AppService) ListUserLocationEvents(ctx context.Context, filter domain.UserLocationListFilter) ([]domain.UserLocationEvent, error) {
	return s.repo.ListUserLocationEvents(ctx, filter)
}

func (s *AppService) ListAuthEvents(ctx context.Context, filter domain.AuthEventListFilter) ([]domain.AuthEvent, error) {
	return s.repo.ListAuthEvents(ctx, filter)
}

func (s *AppService) persistAuthContext(
	ctx context.Context,
	userID string,
	phone string,
	requestID string,
	eventType string,
	device *AuthDeviceInput,
	location *AuthLocationInput,
	reqCtx AuthRequestContext,
) error {
	resolvedDevice, resolvedLocation, err := s.resolveAuthContext(ctx, requestID, device, location)
	if err != nil {
		return err
	}

	if resolvedDevice != nil {
		if _, err := s.repo.UpsertUserDevice(ctx, domain.UserDevice{
			UserID:       userID,
			Phone:        phone,
			DeviceID:     resolvedDevice.DeviceID,
			Platform:     string(resolvedDevice.Platform),
			AppVersion:   resolvedDevice.AppVersion,
			OSVersion:    resolvedDevice.OSVersion,
			DeviceModel:  resolvedDevice.DeviceModel,
			Manufacturer: resolvedDevice.Manufacturer,
			PushToken:    resolvedDevice.PushToken,
		}); err != nil {
			return err
		}
	}

	if resolvedLocation != nil {
		deviceID := ""
		if resolvedDevice != nil {
			deviceID = resolvedDevice.DeviceID
		}
		if err := s.repo.CreateUserLocationEvent(ctx, domain.UserLocationEvent{
			UserID:         userID,
			Phone:          phone,
			DeviceID:       deviceID,
			EventType:      authEventTypeToLocationType(eventType),
			Latitude:       ptrFloat64(resolvedLocation.Latitude),
			Longitude:      ptrFloat64(resolvedLocation.Longitude),
			AccuracyMeters: resolvedLocation.AccuracyMeters,
			Source:         string(resolvedLocation.Source),
			IPAddress:      reqCtx.IPAddress,
			UserAgent:      reqCtx.UserAgent,
		}); err != nil {
			return err
		}
	}

	authEvent := domain.AuthEvent{
		UserID:       userID,
		Phone:        phone,
		EventType:    eventType,
		IPAddress:    reqCtx.IPAddress,
		UserAgent:    reqCtx.UserAgent,
		MetadataJSON: metadataWithRequestID(requestID, nil),
	}
	if resolvedDevice != nil {
		authEvent.DeviceID = resolvedDevice.DeviceID
	}
	return s.repo.CreateAuthEvent(ctx, authEvent)
}

func (s *AppService) resolveAuthContext(
	ctx context.Context,
	requestID string,
	device *AuthDeviceInput,
	location *AuthLocationInput,
) (*domain.AuthDevice, *domain.AuthLocation, error) {
	resolvedDevice, err := normalizeDeviceInput(device)
	if err != nil {
		return nil, nil, err
	}
	resolvedLocation, err := normalizeLocationInput(location)
	if err != nil {
		return nil, nil, err
	}
	if resolvedDevice != nil || resolvedLocation != nil || strings.TrimSpace(requestID) == "" {
		return resolvedDevice, resolvedLocation, nil
	}

	session, err := s.repo.GetTelegramOTPSessionByRequestID(ctx, requestID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return session.Device, session.Location, nil
}

func authEventTypeToLocationType(eventType string) string {
	switch eventType {
	case "register_success":
		return "register"
	case "login_success":
		return "login"
	case "password_reset_success":
		return "password_reset"
	default:
		return "manual_update"
	}
}

func ptrFloat64(value float64) *float64 {
	out := value
	return &out
}
