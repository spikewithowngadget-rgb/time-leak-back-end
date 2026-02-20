package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time-leak/internal/domain"
	"time-leak/internal/repository"

	"go.uber.org/zap"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailAlreadyExists = errors.New("email already exists")
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

func (s *AppService) RegisterUser(email, password, userLanguage string) (domain.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return domain.User{}, errors.New("email and password are required")
	}

	user, err := s.repo.CreateUser(email, hashPassword(password), userLanguage)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.User{}, ErrEmailAlreadyExists
		}
		return domain.User{}, err
	}

	user.Password = ""
	return user, nil
}

func (s *AppService) Login(email, password string) (domain.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	password = strings.TrimSpace(password)
	if email == "" || password == "" {
		return domain.User{}, ErrInvalidCredentials
	}

	user, err := s.repo.GetUserByEmail(email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, err
	}

	if user.Password != hashPassword(password) {
		return domain.User{}, ErrInvalidCredentials
	}

	user.Password = ""
	return user, nil
}

func (s *AppService) GetUser(userID string) (domain.User, error) {
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return domain.User{}, err
	}
	user.Password = ""
	return user, nil
}

func (s *AppService) UpdateUserLanguage(userID, userLanguage string) error {
	return s.repo.UpdateUserLanguage(userID, userLanguage)
}

func (s *AppService) CreateNote(userID, noteType string) (domain.Note, error) {
	return s.repo.CreateNote(userID, noteType)
}

func (s *AppService) ListNotes(userID string) ([]domain.Note, error) {
	return s.repo.ListNotesByUserID(userID)
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
