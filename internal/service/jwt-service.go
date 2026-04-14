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
	RefreshToken     string `json:"refresh_token"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type AccessClaims struct {
	UserUUID string `json:"uid,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Role     string `json:"role"`
	AuthType string `json:"auth_type"`
	jwt.RegisteredClaims
}

type RefreshStore interface {
	Save(ctx context.Context, token string, rec domain.RefreshRecord) error
	Get(ctx context.Context, token string) (domain.RefreshRecord, error)
	Revoke(ctx context.Context, token string) error
	SaveAdminRefresh(ctx context.Context, token string, rec domain.AdminRefreshRecord) error
	GetAdminRefresh(ctx context.Context, token string) (domain.AdminRefreshRecord, error)
	RevokeAdminRefresh(ctx context.Context, token string) error
}

type AuthService struct {
	issuer       string
	accessSecret []byte
	adminSecret  []byte
	accessTTL    time.Duration
	adminTTL     time.Duration
	refreshTTL   time.Duration
	store        RefreshStore
	log          *zap.Logger
}

func NewAuthService(jwtCfg config.JWTConfig, store RefreshStore, log *zap.Logger) *AuthService {
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
		log:          log,
	}
}

func (s *AuthService) AccessTTLSeconds() int {
	return int(s.accessTTL.Seconds())
}

func (s *AuthService) AdminTTLSeconds() int {
	return int(s.adminTTL.Seconds())
}

func (s *AuthService) IssueUserTokens(ctx context.Context, user domain.User, authType string) (TokenPair, error) {
	if strings.TrimSpace(user.UserID) == "" {
		return TokenPair{}, errors.New("user id is empty")
	}
	phone := normalizePhone(user.Phone)
	if phone == "" {
		return TokenPair{}, errors.New("user phone is empty")
	}
	authType = strings.TrimSpace(authType)
	if authType == "" {
		authType = "otp_whatsapp"
	}
	return s.issueTokens(ctx, user.UserID, phone, authType, "user")
}

func (s *AuthService) IssueTestingUserAccessToken(user domain.User, authType string) (string, error) {
	if strings.TrimSpace(user.UserID) == "" {
		return "", errors.New("user id is empty")
	}

	phone := normalizePhone(user.Phone)
	if phone == "" {
		return "", errors.New("user phone is empty")
	}

	authType = strings.TrimSpace(authType)
	if authType == "" {
		authType = "testing_phone"
	}

	now := time.Now().UTC()
	claims := AccessClaims{
		UserUUID: user.UserID,
		Phone:    phone,
		Role:     "user",
		AuthType: authType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   s.issuer,
			Subject:  phone,
			IssuedAt: jwt.NewNumericDate(now),
		},
	}

	return s.signAccessToken(claims, s.accessSecret)
}

func (s *AuthService) IssueAdminToken(ctx context.Context, username string) (AdminToken, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return AdminToken{}, errors.New("username is empty")
	}

	access, err := s.issueAdminAccessToken(username)
	if err != nil {
		return AdminToken{}, err
	}

	refresh, err := newRandomToken(48)
	if err != nil {
		return AdminToken{}, err
	}

	if err := s.store.SaveAdminRefresh(ctx, refresh, domain.AdminRefreshRecord{
		Username:  username,
		ExpiresAt: time.Now().UTC().Add(s.refreshTTL),
		Revoked:   false,
	}); err != nil {
		return AdminToken{}, err
	}

	return AdminToken{
		AccessToken:      access,
		RefreshToken:     refresh,
		ExpiresInSeconds: int(s.adminTTL.Seconds()),
	}, nil
}

func (s *AuthService) issueTokens(
	ctx context.Context,
	userUUID string,
	phone string,
	authType string,
	role string,
) (TokenPair, error) {
	now := time.Now().UTC()
	phone = normalizePhone(phone)
	if phone == "" {
		return TokenPair{}, errors.New("phone is empty")
	}
	authType = strings.TrimSpace(authType)
	if authType == "" {
		authType = "otp_whatsapp"
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}

	claims := AccessClaims{
		UserUUID: userUUID,
		Phone:    phone,
		Role:     role,
		AuthType: authType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   phone,
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
		Phone:     phone,
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

func (s *AuthService) issueUserAccessToken(userUUID, phone, authType, role string) (string, error) {
	now := time.Now().UTC()
	claims := AccessClaims{
		UserUUID: userUUID,
		Phone:    phone,
		Role:     role,
		AuthType: authType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   phone,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	return s.signAccessToken(claims, s.accessSecret)
}

func (s *AuthService) issueAdminAccessToken(username string) (string, error) {
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

	return s.signAccessToken(claims, s.adminSecret)
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
	if err == nil {
		if rec.Revoked {
			return TokenPair{}, ErrRefreshRevoked
		}
		if time.Now().After(rec.ExpiresAt) {
			return TokenPair{}, ErrRefreshExpired
		}

		if strings.TrimSpace(rec.AuthType) == "" {
			rec.AuthType = "otp_whatsapp"
		}
		if strings.TrimSpace(rec.Role) == "" {
			rec.Role = "user"
		}

		// Issue a new access token but reuse the existing refresh token so that
		// concurrent refresh calls from the same client all succeed instead of
		// racing to revoke the token and returning 401.
		access, err := s.issueUserAccessToken(rec.UserUUID, normalizePhone(rec.Phone), rec.AuthType, rec.Role)
		if err != nil {
			return TokenPair{}, err
		}
		return TokenPair{AccessToken: access, RefreshToken: oldRefresh}, nil
	}

	adminRec, err := s.store.GetAdminRefresh(ctx, oldRefresh)
	if err != nil {
		return TokenPair{}, ErrRefreshNotFound
	}
	if adminRec.Revoked {
		return TokenPair{}, ErrRefreshRevoked
	}
	if time.Now().After(adminRec.ExpiresAt) {
		return TokenPair{}, ErrRefreshExpired
	}

	// Reuse the same admin refresh token; only issue a fresh access token.
	adminAccess, err := s.issueAdminAccessToken(adminRec.Username)
	if err != nil {
		return TokenPair{}, err
	}
	return TokenPair{AccessToken: adminAccess, RefreshToken: oldRefresh}, nil
}

func newRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
