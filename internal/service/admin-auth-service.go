package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time-leak/config"

	"go.uber.org/zap"
)

var ErrInvalidAdminCredentials = errors.New("invalid admin credentials")

type AdminLoginResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type AdminTokenIssuer interface {
	IssueAdminToken(ctx context.Context, username string) (AdminToken, error)
}

type AdminAuthService struct {
	username string
	password string
	issuer   AdminTokenIssuer
	log      *zap.Logger
}

func NewAdminAuthService(cfg config.AdminConfig, issuer AdminTokenIssuer, log *zap.Logger) *AdminAuthService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AdminAuthService{
		username: strings.TrimSpace(cfg.Username),
		password: strings.TrimSpace(cfg.Password),
		issuer:   issuer,
		log:      log,
	}
}

func (s *AdminAuthService) Login(ctx context.Context, username, password string) (AdminLoginResponse, error) {
	if !constantTimeEqual(strings.TrimSpace(username), s.username) ||
		!constantTimeEqual(strings.TrimSpace(password), s.password) {
		return AdminLoginResponse{}, ErrInvalidAdminCredentials
	}

	token, err := s.issuer.IssueAdminToken(ctx, s.username)
	if err != nil {
		return AdminLoginResponse{}, err
	}
	return AdminLoginResponse{
		AccessToken:      token.AccessToken,
		RefreshToken:     token.RefreshToken,
		ExpiresInSeconds: token.ExpiresInSeconds,
	}, nil
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
