package service

import (
	"context"
	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/repository"

	"go.uber.org/zap"
)

type IUserNotesService interface {
	RegisterUser(email, password, userLanguage string) (domain.User, error)
	Login(email, password string) (domain.User, error)
	GetUser(userID string) (domain.User, error)
	UpdateUserLanguage(userID, userLanguage string) error
	CreateNote(userID, noteType string) (domain.Note, error)
	ListNotes(userID string) ([]domain.Note, error)
}

type Services struct {
	App IUserNotesService
	JWT IJWTService
}

func NewServices(
	_ context.Context,
	appConfig *config.Config,
	repositories *repository.Repositories,
	log *zap.Logger,
) *Services {
	secret := appConfig.JWT.AccessSecret
	if secret == "" {
		secret = "change-me"
	}

	return &Services{
		App: NewAppService(repositories.Auth, log),
		JWT: NewAuthService(secret, repositories.Auth, repositories.Auth, log),
	}
}
