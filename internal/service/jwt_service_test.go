package service

import (
	"context"
	"errors"
	"testing"
	"time"
	"time-leak/config"
	"time-leak/internal/domain"

	"go.uber.org/zap"
)

type mockRefreshStore struct {
	saveErr   error
	getErr    error
	revokeErr error

	data         map[string]domain.RefreshRecord
	adminData    map[string]domain.AdminRefreshRecord
	revoked      map[string]bool
	adminRevoked map[string]bool
}

func newMockRefreshStore() *mockRefreshStore {
	return &mockRefreshStore{
		data:         make(map[string]domain.RefreshRecord),
		adminData:    make(map[string]domain.AdminRefreshRecord),
		revoked:      make(map[string]bool),
		adminRevoked: make(map[string]bool),
	}
}

func (m *mockRefreshStore) Save(_ context.Context, token string, rec domain.RefreshRecord) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.data[token] = rec
	return nil
}

func (m *mockRefreshStore) Get(_ context.Context, token string) (domain.RefreshRecord, error) {
	if m.getErr != nil {
		return domain.RefreshRecord{}, m.getErr
	}
	rec, ok := m.data[token]
	if !ok {
		return domain.RefreshRecord{}, errors.New("not found")
	}
	return rec, nil
}

func (m *mockRefreshStore) Revoke(_ context.Context, token string) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	m.revoked[token] = true
	rec, ok := m.data[token]
	if ok {
		rec.Revoked = true
		m.data[token] = rec
	}
	return nil
}

func (m *mockRefreshStore) SaveAdminRefresh(_ context.Context, token string, rec domain.AdminRefreshRecord) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.adminData[token] = rec
	return nil
}

func (m *mockRefreshStore) GetAdminRefresh(_ context.Context, token string) (domain.AdminRefreshRecord, error) {
	if m.getErr != nil {
		return domain.AdminRefreshRecord{}, m.getErr
	}
	rec, ok := m.adminData[token]
	if !ok {
		return domain.AdminRefreshRecord{}, errors.New("not found")
	}
	return rec, nil
}

func (m *mockRefreshStore) RevokeAdminRefresh(_ context.Context, token string) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	m.adminRevoked[token] = true
	rec, ok := m.adminData[token]
	if ok {
		rec.Revoked = true
		m.adminData[token] = rec
	}
	return nil
}

func newAuthForTest() (*AuthService, *mockRefreshStore) {
	store := newMockRefreshStore()
	svc := NewAuthService(config.JWTConfig{
		AccessSecret:   "test-secret",
		AdminSecret:    "test-admin-secret",
		AccessTTL:      60 * time.Second,
		AdminAccessTTL: 60 * time.Second,
		RefreshTTL:     24 * time.Hour,
		Issuer:         "test-suite",
	}, store, zap.NewNop())
	return svc, store
}

func TestJWT_IssueUserTokens_Success(t *testing.T) {
	svc, store := newAuthForTest()

	pair, err := svc.IssueUserTokens(context.Background(), domain.User{UserID: "u-1", Phone: "+77015556677"}, "otp_whatsapp")
	if err != nil {
		t.Fatalf("IssueUserTokens error: %v", err)
	}
	if pair.AccessToken == "" {
		t.Fatal("expected access token")
	}
	if pair.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}

	rec, ok := store.data[pair.RefreshToken]
	if !ok {
		t.Fatal("expected refresh token to be saved")
	}
	if rec.Phone != "+77015556677" {
		t.Fatalf("expected normalized phone, got %q", rec.Phone)
	}
	if rec.UserUUID != "u-1" {
		t.Fatalf("unexpected user uuid: %q", rec.UserUUID)
	}
	if rec.AuthType != "otp_whatsapp" {
		t.Fatalf("unexpected auth type: %q", rec.AuthType)
	}
	if rec.Role != "user" {
		t.Fatalf("unexpected role: %q", rec.Role)
	}
}

func TestJWT_IssueUserTokens_EmptyPhone(t *testing.T) {
	svc, _ := newAuthForTest()

	_, err := svc.IssueUserTokens(context.Background(), domain.User{UserID: "u-1"}, "otp_whatsapp")
	if err == nil {
		t.Fatal("expected error for empty phone")
	}
}

func TestJWT_VerifyUserAccess_Success(t *testing.T) {
	svc, _ := newAuthForTest()

	pair, err := svc.IssueUserTokens(context.Background(), domain.User{UserID: "u-1", Phone: "+77015556677"}, "otp_whatsapp")
	if err != nil {
		t.Fatalf("IssueUserTokens error: %v", err)
	}

	claims, err := svc.VerifyUserAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyUserAccess error: %v", err)
	}
	if claims.UserUUID != "u-1" {
		t.Fatalf("unexpected user uuid: %q", claims.UserUUID)
	}
	if claims.Phone != "+77015556677" {
		t.Fatalf("unexpected phone: %q", claims.Phone)
	}
	if claims.AuthType != "otp_whatsapp" {
		t.Fatalf("unexpected auth_type: %q", claims.AuthType)
	}
	if claims.Role != "user" {
		t.Fatalf("unexpected role: %q", claims.Role)
	}
}

func TestJWT_IssueTestingUserAccessToken_Success(t *testing.T) {
	svc, _ := newAuthForTest()

	accessToken, err := svc.IssueTestingUserAccessToken(domain.User{
		UserID: "u-1",
		Phone:  "+77015556677",
	}, "testing_phone_forever")
	if err != nil {
		t.Fatalf("IssueTestingUserAccessToken error: %v", err)
	}
	if accessToken == "" {
		t.Fatal("expected access token")
	}

	claims, err := svc.VerifyUserAccess(accessToken)
	if err != nil {
		t.Fatalf("VerifyUserAccess error: %v", err)
	}
	if claims.UserUUID != "u-1" {
		t.Fatalf("unexpected user uuid: %q", claims.UserUUID)
	}
	if claims.Phone != "+77015556677" {
		t.Fatalf("unexpected phone: %q", claims.Phone)
	}
	if claims.AuthType != "testing_phone_forever" {
		t.Fatalf("unexpected auth_type: %q", claims.AuthType)
	}
	if claims.ExpiresAt != nil {
		t.Fatalf("expected no expiry, got %v", claims.ExpiresAt)
	}
}

func TestJWT_VerifyAdminAccess_Success(t *testing.T) {
	svc, _ := newAuthForTest()
	adminToken, err := svc.IssueAdminToken(context.Background(), "Admin")
	if err != nil {
		t.Fatalf("IssueAdminToken error: %v", err)
	}

	claims, err := svc.VerifyAdminAccess(adminToken.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAdminAccess error: %v", err)
	}
	if claims.Role != "admin" {
		t.Fatalf("unexpected role: %q", claims.Role)
	}
	if claims.AuthType != "admin_login" {
		t.Fatalf("unexpected auth_type: %q", claims.AuthType)
	}
	if adminToken.RefreshToken == "" {
		t.Fatal("expected admin refresh token")
	}
}

func TestJWT_VerifyAccess_InvalidToken(t *testing.T) {
	svc, _ := newAuthForTest()

	_, err := svc.VerifyAccess("not-a-token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestJWT_VerifyAccess_ExpiredToken(t *testing.T) {
	svc, _ := newAuthForTest()
	svc.accessTTL = -1 * time.Second

	pair, err := svc.IssueUserTokens(context.Background(), domain.User{UserID: "u-1", Phone: "+77015556677"}, "otp_whatsapp")
	if err != nil {
		t.Fatalf("IssueUserTokens error: %v", err)
	}

	_, err = svc.VerifyAccess(pair.AccessToken)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestJWT_Refresh_Success(t *testing.T) {
	svc, store := newAuthForTest()

	pair, err := svc.IssueUserTokens(context.Background(), domain.User{UserID: "u-1", Phone: "+77015556677"}, "otp_whatsapp")
	if err != nil {
		t.Fatalf("IssueUserTokens error: %v", err)
	}

	newPair, err := svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	if newPair.AccessToken == "" {
		t.Fatal("expected new access token")
	}
	// Refresh token is reused (no rotation) so concurrent refreshes don't race.
	if newPair.RefreshToken != pair.RefreshToken {
		t.Fatalf("expected same refresh token to be returned, got different")
	}
	if store.revoked[pair.RefreshToken] {
		t.Fatal("refresh token must NOT be revoked after a successful refresh")
	}
}

func TestJWT_Refresh_Idempotent(t *testing.T) {
	svc, _ := newAuthForTest()

	pair, err := svc.IssueUserTokens(context.Background(), domain.User{UserID: "u-1", Phone: "+77015556677"}, "otp_whatsapp")
	if err != nil {
		t.Fatalf("IssueUserTokens error: %v", err)
	}

	// Simulate two concurrent refresh calls with the same token.
	p1, err1 := svc.Refresh(context.Background(), pair.RefreshToken)
	p2, err2 := svc.Refresh(context.Background(), pair.RefreshToken)
	if err1 != nil {
		t.Fatalf("first Refresh error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second concurrent Refresh error: %v", err2)
	}
	if p1.RefreshToken != pair.RefreshToken || p2.RefreshToken != pair.RefreshToken {
		t.Fatal("both calls should return the same (original) refresh token")
	}
}

func TestJWT_Refresh_EmptyToken(t *testing.T) {
	svc, _ := newAuthForTest()

	_, err := svc.Refresh(context.Background(), " ")
	if !errors.Is(err, ErrRefreshNotFound) {
		t.Fatalf("expected ErrRefreshNotFound, got %v", err)
	}
}

func TestJWT_Refresh_NotFound(t *testing.T) {
	svc, _ := newAuthForTest()

	_, err := svc.Refresh(context.Background(), "missing")
	if !errors.Is(err, ErrRefreshNotFound) {
		t.Fatalf("expected ErrRefreshNotFound, got %v", err)
	}
}

func TestJWT_Refresh_Revoked(t *testing.T) {
	svc, store := newAuthForTest()
	store.data["r1"] = domain.RefreshRecord{
		UserUUID:  "u-1",
		Phone:     "+77015556677",
		AuthType:  "otp_whatsapp",
		Role:      "user",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Revoked:   true,
	}

	_, err := svc.Refresh(context.Background(), "r1")
	if !errors.Is(err, ErrRefreshRevoked) {
		t.Fatalf("expected ErrRefreshRevoked, got %v", err)
	}
}

func TestJWT_Refresh_Expired(t *testing.T) {
	svc, store := newAuthForTest()
	store.data["r1"] = domain.RefreshRecord{
		UserUUID:  "u-1",
		Phone:     "+77015556677",
		AuthType:  "otp_whatsapp",
		Role:      "user",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
		Revoked:   false,
	}

	_, err := svc.Refresh(context.Background(), "r1")
	if !errors.Is(err, ErrRefreshExpired) {
		t.Fatalf("expected ErrRefreshExpired, got %v", err)
	}
	// Expired tokens are rejected but not explicitly revoked (they expire naturally).
	if store.revoked["r1"] {
		t.Fatal("expired refresh should not be explicitly revoked")
	}
}

func TestJWT_Refresh_AdminSuccess(t *testing.T) {
	svc, store := newAuthForTest()

	adminToken, err := svc.IssueAdminToken(context.Background(), "Admin")
	if err != nil {
		t.Fatalf("IssueAdminToken error: %v", err)
	}

	newPair, err := svc.Refresh(context.Background(), adminToken.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	if newPair.AccessToken == "" {
		t.Fatal("expected refreshed admin access token")
	}
	// Refresh token is reused (no rotation).
	if newPair.RefreshToken != adminToken.RefreshToken {
		t.Fatalf("expected same admin refresh token to be returned")
	}
	if store.adminRevoked[adminToken.RefreshToken] {
		t.Fatal("admin refresh token must NOT be revoked after a successful refresh")
	}

	claims, err := svc.VerifyAdminAccess(newPair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAdminAccess error: %v", err)
	}
	if claims.Role != "admin" {
		t.Fatalf("unexpected role: %q", claims.Role)
	}
}
