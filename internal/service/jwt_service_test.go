package service

import (
	"errors"
	"testing"
	"time"
	"time-leak/internal/domain"

	"go.uber.org/zap"
)

type mockUserRepo struct {
	uuid string
	err  error
}

func (m *mockUserRepo) GetOrCreateUUIDByEmail(_ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.uuid, nil
}

type mockRefreshStore struct {
	saveErr   error
	getErr    error
	revokeErr error

	data    map[string]domain.RefreshRecord
	revoked map[string]bool
}

func newMockRefreshStore() *mockRefreshStore {
	return &mockRefreshStore{
		data:    make(map[string]domain.RefreshRecord),
		revoked: make(map[string]bool),
	}
}

func (m *mockRefreshStore) Save(token string, rec domain.RefreshRecord) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.data[token] = rec
	return nil
}

func (m *mockRefreshStore) Get(token string) (domain.RefreshRecord, error) {
	if m.getErr != nil {
		return domain.RefreshRecord{}, m.getErr
	}
	rec, ok := m.data[token]
	if !ok {
		return domain.RefreshRecord{}, errors.New("not found")
	}
	return rec, nil
}

func (m *mockRefreshStore) Revoke(token string) error {
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

func newAuthForTest() (*AuthService, *mockRefreshStore, *mockUserRepo) {
	store := newMockRefreshStore()
	users := &mockUserRepo{uuid: "11111111-1111-1111-1111-111111111111"}
	svc := NewAuthService("test-secret", store, users, zap.NewNop())
	svc.accessTTL = 2 * time.Second
	svc.refreshTTL = 24 * time.Hour
	return svc, store, users
}

func TestJWT_IssueTokensByEmail_Success(t *testing.T) {
	svc, store, _ := newAuthForTest()

	pair, err := svc.IssueTokensByEmail("Demo@Example.com ")
	if err != nil {
		t.Fatalf("IssueTokensByEmail error: %v", err)
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
	if rec.Email != "demo@example.com" {
		t.Fatalf("expected normalized email, got %q", rec.Email)
	}
	if rec.UserUUID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected user uuid: %q", rec.UserUUID)
	}
}

func TestJWT_IssueTokensByEmail_EmptyEmail(t *testing.T) {
	svc, _, _ := newAuthForTest()

	_, err := svc.IssueTokensByEmail("   ")
	if err == nil {
		t.Fatal("expected error for empty email")
	}
}

func TestJWT_VerifyAccess_Success(t *testing.T) {
	svc, _, _ := newAuthForTest()
	pair, err := svc.IssueTokens("u-1", "a@b.com")
	if err != nil {
		t.Fatalf("IssueTokens error: %v", err)
	}

	claims, err := svc.VerifyAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccess error: %v", err)
	}
	if claims.UserUUID != "u-1" {
		t.Fatalf("unexpected user uuid: %q", claims.UserUUID)
	}
	if claims.Email != "a@b.com" {
		t.Fatalf("unexpected email: %q", claims.Email)
	}
}

func TestJWT_VerifyAccess_InvalidToken(t *testing.T) {
	svc, _, _ := newAuthForTest()

	_, err := svc.VerifyAccess("not-a-token")
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestJWT_VerifyAccess_ExpiredToken(t *testing.T) {
	svc, _, _ := newAuthForTest()
	svc.accessTTL = -1 * time.Second

	pair, err := svc.IssueTokens("u-1", "a@b.com")
	if err != nil {
		t.Fatalf("IssueTokens error: %v", err)
	}

	_, err = svc.VerifyAccess(pair.AccessToken)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestJWT_Refresh_Success(t *testing.T) {
	svc, store, _ := newAuthForTest()
	pair, err := svc.IssueTokens("u-1", "a@b.com")
	if err != nil {
		t.Fatalf("IssueTokens error: %v", err)
	}

	newPair, err := svc.Refresh(pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	if newPair.AccessToken == "" || newPair.RefreshToken == "" {
		t.Fatal("expected new token pair")
	}
	if !store.revoked[pair.RefreshToken] {
		t.Fatal("expected old refresh to be revoked")
	}
}

func TestJWT_Refresh_EmptyToken(t *testing.T) {
	svc, _, _ := newAuthForTest()

	_, err := svc.Refresh(" ")
	if !errors.Is(err, ErrRefreshNotFound) {
		t.Fatalf("expected ErrRefreshNotFound, got %v", err)
	}
}

func TestJWT_Refresh_NotFound(t *testing.T) {
	svc, _, _ := newAuthForTest()

	_, err := svc.Refresh("missing")
	if !errors.Is(err, ErrRefreshNotFound) {
		t.Fatalf("expected ErrRefreshNotFound, got %v", err)
	}
}

func TestJWT_Refresh_Revoked(t *testing.T) {
	svc, store, _ := newAuthForTest()
	store.data["r1"] = domain.RefreshRecord{
		UserUUID:  "u-1",
		Email:     "a@b.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Revoked:   true,
	}

	_, err := svc.Refresh("r1")
	if !errors.Is(err, ErrRefreshRevoked) {
		t.Fatalf("expected ErrRefreshRevoked, got %v", err)
	}
}

func TestJWT_Refresh_Expired(t *testing.T) {
	svc, store, _ := newAuthForTest()
	store.data["r1"] = domain.RefreshRecord{
		UserUUID:  "u-1",
		Email:     "a@b.com",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
		Revoked:   false,
	}

	_, err := svc.Refresh("r1")
	if !errors.Is(err, ErrRefreshExpired) {
		t.Fatalf("expected ErrRefreshExpired, got %v", err)
	}
	if !store.revoked["r1"] {
		t.Fatal("expected expired refresh to be revoked")
	}
}
