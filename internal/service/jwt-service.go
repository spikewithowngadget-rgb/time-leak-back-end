package service

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"time-leak/internal/domain"

	"go.uber.org/zap"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken    = errors.New("invalid token")
	ErrExpiredToken    = errors.New("token expired")
	ErrRefreshNotFound = errors.New("refresh not found")
	ErrRefreshRevoked  = errors.New("refresh revoked")
	ErrRefreshExpired  = errors.New("refresh expired")
)

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type AccessClaims struct {
	UserUUID string `json:"uid"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

type IJWTService interface {
	IssueTokensByEmail(email string) (TokenPair, error)
	VerifyAccess(accessToken string) (*AccessClaims, error)
	Refresh(oldRefresh string) (TokenPair, error)
}

type RefreshStore interface {
	Save(token string, rec domain.RefreshRecord) error
	Get(token string) (domain.RefreshRecord, error)
	Revoke(token string) error
}

type UserRepo interface {
	GetOrCreateUUIDByEmail(email string) (string, error)
}

type AuthService struct {
	issuer       string
	accessSecret []byte
	accessTTL    time.Duration
	refreshTTL   time.Duration
	store        RefreshStore
	users        UserRepo
	log          *zap.Logger
}

func NewAuthService(accessSecret string, store RefreshStore, users UserRepo, log *zap.Logger) *AuthService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AuthService{
		issuer:       "tax-bot",
		accessSecret: []byte(accessSecret),
		accessTTL:    60 * time.Second,
		refreshTTL:   30 * 24 * time.Hour,
		store:        store,
		users:        users,
		log:          log,
	}
}

func (s *AuthService) Daemon() {
	// no-op сейчас.
	// Если захочешь — сюда можно добавить:
	// - периодическую чистку refresh_tokens по expires_at
	// - метрики, логирование, и т.д.
}

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func (s *AuthService) IssueTokensByEmail(email string) (TokenPair, error) {
	email = normalizeEmail(email)
	if email == "" {
		return TokenPair{}, errors.New("email is empty")
	}

	userUUID, err := s.users.GetOrCreateUUIDByEmail(email)
	if err != nil {
		return TokenPair{}, err
	}
	return s.IssueTokens(userUUID, email)
}

// Внутренний метод (не обязательно держать в интерфейсе, но можно оставить публичным)
func (s *AuthService) IssueTokens(userUUID, email string) (TokenPair, error) {
	now := time.Now()

	claims := AccessClaims{
		UserUUID: userUUID,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	access, err := t.SignedString(s.accessSecret)
	if err != nil {
		return TokenPair{}, err
	}

	refresh, err := newRandomToken(48)
	if err != nil {
		return TokenPair{}, err
	}

	if err := s.store.Save(refresh, domain.RefreshRecord{
		UserUUID:  userUUID,
		Email:     email,
		ExpiresAt: now.Add(s.refreshTTL),
		Revoked:   false,
	}); err != nil {
		return TokenPair{}, err
	}

	return TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (s *AuthService) VerifyAccess(accessToken string) (*AccessClaims, error) {
	tok, err := jwt.ParseWithClaims(accessToken, &AccessClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return s.accessSecret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	claims, ok := tok.Claims.(*AccessClaims)
	if !ok || !tok.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func (s *AuthService) Refresh(oldRefresh string) (TokenPair, error) {
	oldRefresh = strings.TrimSpace(oldRefresh)
	if oldRefresh == "" {
		return TokenPair{}, ErrRefreshNotFound
	}

	rec, err := s.store.Get(oldRefresh)
	if err != nil {
		return TokenPair{}, ErrRefreshNotFound
	}
	if rec.Revoked {
		return TokenPair{}, ErrRefreshRevoked
	}
	if time.Now().After(rec.ExpiresAt) {
		_ = s.store.Revoke(oldRefresh)
		return TokenPair{}, ErrRefreshExpired
	}

	if err := s.store.Revoke(oldRefresh); err != nil {
		return TokenPair{}, err
	}

	return s.IssueTokens(rec.UserUUID, normalizeEmail(rec.Email))
}

func newRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
