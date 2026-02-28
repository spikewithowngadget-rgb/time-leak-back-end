package service

import (
	"context"
	"database/sql"
	"testing"
	"time"
	"time-leak/internal/domain"

	"go.uber.org/zap"
)

type adsRepoMock struct {
	ads   []domain.Ad
	state map[string]domain.UserAdState
}

func newAdsRepoMock() *adsRepoMock {
	return &adsRepoMock{state: make(map[string]domain.UserAdState)}
}

func (m *adsRepoMock) CreateAd(_ context.Context, in domain.AdCreateInput) (domain.Ad, error) {
	ad := domain.Ad{
		ID:        in.Title,
		Title:     in.Title,
		ImageURL:  in.ImageURL,
		TargetURL: in.TargetURL,
		IsActive:  in.IsActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	m.ads = append(m.ads, ad)
	return ad, nil
}

func (m *adsRepoMock) UpdateAd(_ context.Context, id string, in domain.AdUpdateInput) (domain.Ad, error) {
	for i := range m.ads {
		if m.ads[i].ID == id {
			if in.Title != nil {
				m.ads[i].Title = *in.Title
			}
			if in.ImageURL != nil {
				m.ads[i].ImageURL = *in.ImageURL
			}
			if in.TargetURL != nil {
				m.ads[i].TargetURL = *in.TargetURL
			}
			if in.IsActive != nil {
				m.ads[i].IsActive = *in.IsActive
			}
			m.ads[i].UpdatedAt = time.Now().UTC()
			return m.ads[i], nil
		}
	}
	return domain.Ad{}, sql.ErrNoRows
}

func (m *adsRepoMock) DeleteAd(_ context.Context, id string) error {
	for i := range m.ads {
		if m.ads[i].ID == id {
			m.ads = append(m.ads[:i], m.ads[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (m *adsRepoMock) ListAds(_ context.Context, _, _ int, active *bool) ([]domain.Ad, error) {
	if active == nil {
		return append([]domain.Ad(nil), m.ads...), nil
	}
	result := make([]domain.Ad, 0, len(m.ads))
	for _, ad := range m.ads {
		if ad.IsActive == *active {
			result = append(result, ad)
		}
	}
	return result, nil
}

func (m *adsRepoMock) ListActiveAds(ctx context.Context) ([]domain.Ad, error) {
	active := true
	return m.ListAds(ctx, 0, 0, &active)
}

func (m *adsRepoMock) GetUserAdState(_ context.Context, userID string) (domain.UserAdState, error) {
	state, ok := m.state[userID]
	if !ok {
		return domain.UserAdState{}, sql.ErrNoRows
	}
	return state, nil
}

func (m *adsRepoMock) UpsertUserAdState(_ context.Context, userID, lastAdID string, updatedAt time.Time) error {
	m.state[userID] = domain.UserAdState{UserID: userID, LastAdID: lastAdID, UpdatedAt: updatedAt}
	return nil
}

func TestAdsRotation_NextAd_RotatesPerUser(t *testing.T) {
	repo := newAdsRepoMock()
	repo.ads = []domain.Ad{
		{ID: "ad-1", Title: "Ad 1", IsActive: true},
		{ID: "ad-2", Title: "Ad 2", IsActive: true},
		{ID: "ad-3", Title: "Ad 3", IsActive: true},
	}
	svc := NewAdsService(repo, zap.NewNop())
	ctx := context.Background()

	ad1, err := svc.NextAdForUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("NextAdForUser #1 error: %v", err)
	}
	ad2, err := svc.NextAdForUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("NextAdForUser #2 error: %v", err)
	}
	ad3, err := svc.NextAdForUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("NextAdForUser #3 error: %v", err)
	}
	ad4, err := svc.NextAdForUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("NextAdForUser #4 error: %v", err)
	}

	if ad1.ID != "ad-1" || ad2.ID != "ad-2" || ad3.ID != "ad-3" || ad4.ID != "ad-1" {
		t.Fatalf("unexpected rotation order: %s, %s, %s, %s", ad1.ID, ad2.ID, ad3.ID, ad4.ID)
	}
}

func TestAdsRotation_NextAd_HandlesMissingLastAd(t *testing.T) {
	repo := newAdsRepoMock()
	repo.ads = []domain.Ad{
		{ID: "ad-a", Title: "A", IsActive: true},
		{ID: "ad-b", Title: "B", IsActive: true},
	}
	repo.state["user-2"] = domain.UserAdState{UserID: "user-2", LastAdID: "deleted-ad", UpdatedAt: time.Now().UTC()}

	svc := NewAdsService(repo, zap.NewNop())
	ad, err := svc.NextAdForUser(context.Background(), "user-2")
	if err != nil {
		t.Fatalf("NextAdForUser error: %v", err)
	}
	if ad.ID != "ad-a" {
		t.Fatalf("expected fallback to first ad, got %q", ad.ID)
	}
}

func TestAdsRotation_NextAd_NoActiveAds(t *testing.T) {
	repo := newAdsRepoMock()
	repo.ads = []domain.Ad{{ID: "ad-x", IsActive: false}}
	svc := NewAdsService(repo, zap.NewNop())

	_, err := svc.NextAdForUser(context.Background(), "user-3")
	if err != ErrNoActiveAds {
		t.Fatalf("expected ErrNoActiveAds, got %v", err)
	}
}
