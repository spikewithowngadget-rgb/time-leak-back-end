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
	GetUserByPhone(ctx context.Context, phone string) (domain.User, error)
	UpdateUserLanguage(ctx context.Context, userID, userLanguage string) error
	CreateNote(ctx context.Context, userID, noteType string, noteFiles []string) (domain.Note, error)
	GetNote(ctx context.Context, noteID, userID string) (domain.Note, error)
	UpdateNote(ctx context.Context, noteID, userID, noteType string, noteFiles []string) (domain.Note, error)
	DeleteNote(ctx context.Context, noteID, userID string) error
	ListNotes(ctx context.Context, userID string) ([]domain.Note, error)
	CreateAuthVerification(
		ctx context.Context,
		purpose domain.AuthVerificationPurpose,
		requestID string,
		phone string,
	) (AuthVerificationToken, error)
	RegisterWithPhoneOTP(
		ctx context.Context,
		phone string,
		password string,
		confirmPassword string,
		verificationToken string,
		device *AuthDeviceInput,
		location *AuthLocationInput,
		reqCtx AuthRequestContext,
	) (domain.User, error)
	LoginByPhonePassword(
		ctx context.Context,
		phone string,
		password string,
		verificationToken string,
		device *AuthDeviceInput,
		location *AuthLocationInput,
		reqCtx AuthRequestContext,
	) (domain.User, error)
	ResetPasswordWithOTP(
		ctx context.Context,
		phone string,
		newPassword string,
		confirmPassword string,
		verificationToken string,
		device *AuthDeviceInput,
		location *AuthLocationInput,
		reqCtx AuthRequestContext,
	) error
	DeactivateUser(ctx context.Context, userID string) error
	ListUserDevices(ctx context.Context, userID string) ([]domain.UserDevice, error)
	DeactivateUserDevice(ctx context.Context, userID, deviceID string) error
	ListUserLocationEvents(ctx context.Context, filter domain.UserLocationListFilter) ([]domain.UserLocationEvent, error)
	ListAuthEvents(ctx context.Context, filter domain.AuthEventListFilter) ([]domain.AuthEvent, error)
}

type IJWTService interface {
	IssueUserTokens(ctx context.Context, user domain.User, authType string) (TokenPair, error)
	IssueTestingUserAccessToken(user domain.User, authType string) (string, error)
	IssueAdminToken(ctx context.Context, username string) (AdminToken, error)
	VerifyAccess(accessToken string) (*AccessClaims, error)
	VerifyUserAccess(accessToken string) (*AccessClaims, error)
	VerifyAdminAccess(accessToken string) (*AccessClaims, error)
	Refresh(ctx context.Context, oldRefresh string) (TokenPair, error)
	AccessTTLSeconds() int
	AdminTTLSeconds() int
}

type IOTPService interface {
	RequestOTP(ctx context.Context, channel domain.OTPChannel, destination string) (domain.OTPRequestResult, error)
	RequestOTPForPurpose(
		ctx context.Context,
		channel domain.OTPChannel,
		destination string,
		purpose domain.AuthVerificationPurpose,
	) (domain.OTPRequestResult, error)
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

type IAnalyticsService interface {
	Overview(ctx context.Context) (domain.AnalyticsOverview, error)
	Geo(ctx context.Context, filter domain.GeoFilter) (domain.GeoResult, error)
	ListDevices(ctx context.Context, filter domain.AdminDeviceListFilter) ([]domain.UserDevice, int, error)
}

type Services struct {
	App       IUserNotesService
	JWT       IJWTService
	OTP       IOTPService
	Ads       IAdsService
	Admin     IAdminAuthService
	Analytics IAnalyticsService
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

	otpCfg := appConfig.OTP
	if otpCfg.AppEnv == "" {
		otpCfg.AppEnv = appConfig.AppEnv
	}

	appService := NewAppService(repositories.Auth, appConfig.OTP.ExpiresIn, log)
	jwtService := NewAuthService(appConfig.JWT, repositories.Auth, log)
	otpService := NewOTPService(repositories.Auth, otpCfg, log, NewWhapiSender(appConfig.Whapi, appConfig.OTP.ExpiresIn, log))
	adsService := NewAdsService(repositories.Auth, log)
	adminService := NewAdminAuthService(appConfig.Admin, jwtService, log)
	analyticsService := NewAnalyticsService(repositories.Auth, log)

	return &Services{
		App:       appService,
		JWT:       jwtService,
		OTP:       otpService,
		Ads:       adsService,
		Admin:     adminService,
		Analytics: analyticsService,
	}
}
