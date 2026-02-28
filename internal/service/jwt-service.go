package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"time-leak/config"
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
	ErrForbiddenRole   = errors.New("forbidden role")
	ErrInvalidAuthType = errors.New("invalid auth type")
)

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type AdminToken struct {
	AccessToken      string `json:"access_token"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type AccessClaims struct {
	UserUUID string `json:"uid,omitempty"`
	Email    string `json:"email,omitempty"`
	Role     string `json:"role"`
	AuthType string `json:"auth_type"`
	jwt.RegisteredClaims
}

type RefreshStore interface {
	Save(ctx context.Context, token string, rec domain.RefreshRecord) error
	Get(ctx context.Context, token string) (domain.RefreshRecord, error)
	Revoke(ctx context.Context, token string) error
}

type UserRepo interface {
	GetOrCreateUUIDByEmail(ctx context.Context, email string) (string, error)
}

type AuthService struct {
	issuer       string
	accessSecret []byte
	adminSecret  []byte
	accessTTL    time.Duration
	adminTTL     time.Duration
	refreshTTL   time.Duration
	store        RefreshStore
	users        UserRepo
	log          *zap.Logger
}

func NewAuthService(jwtCfg config.JWTConfig, store RefreshStore, users UserRepo, log *zap.Logger) *AuthService {
	if log == nil {
		log = zap.NewNop()
	}

	accessTTL := 60 * time.Second
	if jwtCfg.AccessTTL > 0 {
		accessTTL = jwtCfg.AccessTTL
	}
	if accessTTL != 60*time.Second {
		accessTTL = 60 * time.Second
	}

	adminTTL := jwtCfg.AdminAccessTTL
	if adminTTL <= 0 {
		adminTTL = 60 * time.Second
	}

	refreshTTL := jwtCfg.RefreshTTL
	if refreshTTL <= 0 {
		refreshTTL = 30 * 24 * time.Hour
	}

	issuer := strings.TrimSpace(jwtCfg.Issuer)
	if issuer == "" {
		issuer = "time-leak"
	}

	adminSecret := strings.TrimSpace(jwtCfg.AdminSecret)
	if adminSecret == "" {
		adminSecret = strings.TrimSpace(jwtCfg.AccessSecret)
	}

	return &AuthService{
		issuer:       issuer,
		accessSecret: []byte(strings.TrimSpace(jwtCfg.AccessSecret)),
		adminSecret:  []byte(adminSecret),
		accessTTL:    accessTTL,
		adminTTL:     adminTTL,
		refreshTTL:   refreshTTL,
		store:        store,
		users:        users,
		log:          log,
	}
}

func (s *AuthService) AccessTTLSeconds() int {
	return int(s.accessTTL.Seconds())
}

func (s *AuthService) AdminTTLSeconds() int {
	return int(s.adminTTL.Seconds())
}

func (s *AuthService) IssueTokensByEmail(ctx context.Context, email string) (TokenPair, error) {
	email = normalizeEmail(email)
	if email == "" {
		return TokenPair{}, errors.New("email is empty")
	}

	userUUID, err := s.users.GetOrCreateUUIDByEmail(ctx, email)
	if err != nil {
		return TokenPair{}, err
	}
	return s.issueTokens(ctx, userUUID, email, "password", "user")
}

func (s *AuthService) IssueUserTokens(ctx context.Context, user domain.User, authType string) (TokenPair, error) {
	if strings.TrimSpace(user.UserID) == "" {
		return TokenPair{}, errors.New("user id is empty")
	}
	email := normalizeEmail(user.Email)
	if email == "" {
		return TokenPair{}, errors.New("user email is empty")
	}
	authType = strings.TrimSpace(authType)
	if authType == "" {
		authType = "password"
	}
	return s.issueTokens(ctx, user.UserID, email, authType, "user")
}

func (s *AuthService) IssueAdminToken(username string) (AdminToken, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return AdminToken{}, errors.New("username is empty")
	}

	now := time.Now().UTC()
	claims := AccessClaims{
		Role:     "admin",
		AuthType: "admin_login",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.adminTTL)),
		},
	}

	access, err := s.signAccessToken(claims, s.adminSecret)
	if err != nil {
		return AdminToken{}, err
	}

	return AdminToken{AccessToken: access, ExpiresInSeconds: int(s.adminTTL.Seconds())}, nil
}

func (s *AuthService) issueTokens(
	ctx context.Context,
	userUUID string,
	email string,
	authType string,
	role string,
) (TokenPair, error) {
	now := time.Now().UTC()
	authType = strings.TrimSpace(authType)
	if authType == "" {
		authType = "password"
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}

	claims := AccessClaims{
		UserUUID: userUUID,
		Email:    email,
		Role:     role,
		AuthType: authType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}

	access, err := s.signAccessToken(claims, s.accessSecret)
	if err != nil {
		return TokenPair{}, err
	}

	refresh, err := newRandomToken(48)
	if err != nil {
		return TokenPair{}, err
	}

	if err := s.store.Save(ctx, refresh, domain.RefreshRecord{
		UserUUID:  userUUID,
		Email:     email,
		AuthType:  authType,
		Role:      role,
		ExpiresAt: now.Add(s.refreshTTL),
		Revoked:   false,
	}); err != nil {
		return TokenPair{}, err
	}

	return TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (s *AuthService) signAccessToken(claims AccessClaims, secret []byte) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(secret)
}

func (s *AuthService) VerifyAccess(accessToken string) (*AccessClaims, error) {
	claims, err := s.parseWithSecret(accessToken, s.accessSecret)
	if err == nil {
		return claims, nil
	}
	if errors.Is(err, ErrExpiredToken) {
		return nil, err
	}

	adminClaims, adminErr := s.parseWithSecret(accessToken, s.adminSecret)
	if adminErr != nil {
		if errors.Is(adminErr, ErrExpiredToken) {
			return nil, adminErr
		}
		return nil, ErrInvalidToken
	}
	return adminClaims, nil
}

func (s *AuthService) VerifyUserAccess(accessToken string) (*AccessClaims, error) {
	claims, err := s.VerifyAccess(accessToken)
	if err != nil {
		return nil, err
	}
	if claims.Role != "user" {
		return nil, ErrForbiddenRole
	}
	return claims, nil
}

func (s *AuthService) VerifyAdminAccess(accessToken string) (*AccessClaims, error) {
	claims, err := s.VerifyAccess(accessToken)
	if err != nil {
		return nil, err
	}
	if claims.Role != "admin" {
		return nil, ErrForbiddenRole
	}
	return claims, nil
}

func (s *AuthService) parseWithSecret(accessToken string, secret []byte) (*AccessClaims, error) {
	tok, err := jwt.ParseWithClaims(accessToken, &AccessClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return secret, nil
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

func (s *AuthService) Refresh(ctx context.Context, oldRefresh string) (TokenPair, error) {
	oldRefresh = strings.TrimSpace(oldRefresh)
	if oldRefresh == "" {
		return TokenPair{}, ErrRefreshNotFound
	}

	rec, err := s.store.Get(ctx, oldRefresh)
	if err != nil {
		return TokenPair{}, ErrRefreshNotFound
	}
	if rec.Revoked {
		return TokenPair{}, ErrRefreshRevoked
	}
	if time.Now().After(rec.ExpiresAt) {
		_ = s.store.Revoke(ctx, oldRefresh)
		return TokenPair{}, ErrRefreshExpired
	}

	if err := s.store.Revoke(ctx, oldRefresh); err != nil {
		return TokenPair{}, err
	}

	if strings.TrimSpace(rec.AuthType) == "" {
		rec.AuthType = "password"
	}
	if strings.TrimSpace(rec.Role) == "" {
		rec.Role = "user"
	}

	return s.issueTokens(ctx, rec.UserUUID, normalizeEmail(rec.Email), rec.AuthType, rec.Role)
}

func newRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
