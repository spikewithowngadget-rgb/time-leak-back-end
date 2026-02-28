package service

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
	"time-leak/internal/domain"

	"go.uber.org/zap"
)

var (
	ErrAdNotFound     = errors.New("ad not found")
	ErrNoActiveAds    = errors.New("no active ads")
	ErrInvalidAdTitle = errors.New("ad title is required")
	ErrInvalidAdURL   = errors.New("ad urls must be valid http/https URLs")
)

type AdsRepository interface {
	CreateAd(ctx context.Context, in domain.AdCreateInput) (domain.Ad, error)
	UpdateAd(ctx context.Context, id string, in domain.AdUpdateInput) (domain.Ad, error)
	DeleteAd(ctx context.Context, id string) error
	ListAds(ctx context.Context, limit, offset int, active *bool) ([]domain.Ad, error)
	ListActiveAds(ctx context.Context) ([]domain.Ad, error)
	GetUserAdState(ctx context.Context, userID string) (domain.UserAdState, error)
	UpsertUserAdState(ctx context.Context, userID, lastAdID string, updatedAt time.Time) error
}

type AdsService struct {
	repo AdsRepository
	log  *zap.Logger
}

func NewAdsService(repo AdsRepository, log *zap.Logger) *AdsService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AdsService{repo: repo, log: log}
}

func (s *AdsService) CreateAd(ctx context.Context, in domain.AdCreateInput) (domain.Ad, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.ImageURL = strings.TrimSpace(in.ImageURL)
	in.TargetURL = strings.TrimSpace(in.TargetURL)

	if in.Title == "" {
		return domain.Ad{}, ErrInvalidAdTitle
	}
	if !isValidHTTPURL(in.ImageURL) || !isValidHTTPURL(in.TargetURL) {
		return domain.Ad{}, ErrInvalidAdURL
	}

	return s.repo.CreateAd(ctx, in)
}

func (s *AdsService) UpdateAd(ctx context.Context, id string, in domain.AdUpdateInput) (domain.Ad, error) {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return domain.Ad{}, ErrInvalidAdTitle
		}
		in.Title = &t
	}
	if in.ImageURL != nil {
		u := strings.TrimSpace(*in.ImageURL)
		if !isValidHTTPURL(u) {
			return domain.Ad{}, ErrInvalidAdURL
		}
		in.ImageURL = &u
	}
	if in.TargetURL != nil {
		u := strings.TrimSpace(*in.TargetURL)
		if !isValidHTTPURL(u) {
			return domain.Ad{}, ErrInvalidAdURL
		}
		in.TargetURL = &u
	}

	ad, err := s.repo.UpdateAd(ctx, strings.TrimSpace(id), in)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Ad{}, ErrAdNotFound
		}
		return domain.Ad{}, err
	}
	return ad, nil
}

func (s *AdsService) DeleteAd(ctx context.Context, id string) error {
	err := s.repo.DeleteAd(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAdNotFound
		}
		return err
	}
	return nil
}

func (s *AdsService) ListAds(ctx context.Context, limit, offset int, active *bool) ([]domain.Ad, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListAds(ctx, limit, offset, active)
}

func (s *AdsService) NextAdForUser(ctx context.Context, userID string) (domain.Ad, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return domain.Ad{}, errors.New("user id is empty")
	}

	activeAds, err := s.repo.ListActiveAds(ctx)
	if err != nil {
		return domain.Ad{}, err
	}
	if len(activeAds) == 0 {
		return domain.Ad{}, ErrNoActiveAds
	}

	nextIndex := 0
	state, err := s.repo.GetUserAdState(ctx, userID)
	if err == nil {
		idx := findAdIndexByID(activeAds, state.LastAdID)
		if idx >= 0 {
			nextIndex = (idx + 1) % len(activeAds)
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.Ad{}, err
	}

	nextAd := activeAds[nextIndex]
	if err := s.repo.UpsertUserAdState(ctx, userID, nextAd.ID, time.Now().UTC()); err != nil {
		return domain.Ad{}, err
	}
	return nextAd, nil
}

func findAdIndexByID(ads []domain.Ad, id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1
	}
	for i := range ads {
		if ads[i].ID == id {
			return i
		}
	}
	return -1
}

func isValidHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}
