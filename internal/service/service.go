package service

import (
	"context"
	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/repository"

	"go.uber.org/zap"
)

type IUserNotesService interface {
	GetUser(ctx context.Context, userID string) (domain.User, error)
	UpdateUserLanguage(ctx context.Context, userID, userLanguage string) error
	CreateNote(ctx context.Context, userID, noteType string) (domain.Note, error)
	ListNotes(ctx context.Context, userID string) ([]domain.Note, error)
	ResolveUserByPhoneOTP(ctx context.Context, phone string) (domain.User, error)
}

type IJWTService interface {
	IssueUserTokens(ctx context.Context, user domain.User, authType string) (TokenPair, error)
	IssueAdminToken(username string) (AdminToken, error)
	VerifyAccess(accessToken string) (*AccessClaims, error)
	VerifyUserAccess(accessToken string) (*AccessClaims, error)
	VerifyAdminAccess(accessToken string) (*AccessClaims, error)
	Refresh(ctx context.Context, oldRefresh string) (TokenPair, error)
	AccessTTLSeconds() int
	AdminTTLSeconds() int
}

type IOTPService interface {
	RequestOTP(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPRequestResult, error)
	VerifyOTP(ctx context.Context, requestID, code string) (domain.OTPVerifyResult, error)
	GetLatestTestingOTP(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPTestingCode, error)
}

type IAdsService interface {
	CreateAd(ctx context.Context, in domain.AdCreateInput) (domain.Ad, error)
	UpdateAd(ctx context.Context, id string, in domain.AdUpdateInput) (domain.Ad, error)
	DeleteAd(ctx context.Context, id string) error
	ListAds(ctx context.Context, limit, offset int, active *bool) ([]domain.Ad, error)
	NextAdForUser(ctx context.Context, userID string) (domain.Ad, error)
}

type IAdminAuthService interface {
	Login(ctx context.Context, username, password string) (AdminLoginResponse, error)
}

type Services struct {
	App   IUserNotesService
	JWT   IJWTService
	OTP   IOTPService
	Ads   IAdsService
	Admin IAdminAuthService
}

func NewServices(
	_ context.Context,
	appConfig *config.Config,
	repositories *repository.Repositories,
	log *zap.Logger,
) *Services {
	if log == nil {
		log = zap.NewNop()
	}

	appService := NewAppService(repositories.Auth, log)
	jwtService := NewAuthService(appConfig.JWT, repositories.Auth, log)
	otpService := NewOTPService(repositories.Auth, appConfig.OTP, log)
	adsService := NewAdsService(repositories.Auth, log)
	adminService := NewAdminAuthService(appConfig.Admin, jwtService, log)

	return &Services{
		App:   appService,
		JWT:   jwtService,
		OTP:   otpService,
		Ads:   adsService,
		Admin: adminService,
	}
}
