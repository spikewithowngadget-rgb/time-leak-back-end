package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type JWTConfig struct {
	AccessSecret   string
	AdminSecret    string
	AccessTTL      time.Duration
	AdminAccessTTL time.Duration
	RefreshTTL     time.Duration
	Issuer         string
}

type OTPConfig struct {
	HMACSecret      string
	RequestCooldown time.Duration
	MaxAttempts     int
	LockDuration    time.Duration
	ExpiresIn       time.Duration
}

type AdminConfig struct {
	Username string
	Password string
}

type Config struct {
	Addr                   string
	DBPath                 string
	DBName                 string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetime        time.Duration
	EnableTestingEndpoints bool
	JWT                    JWTConfig
	OTP                    OTPConfig
	Admin                  AdminConfig
}

func NewConfig() (*Config, error) {
	cfg := &Config{
		Addr:            ":8081",
		DBPath:          "data",
		DBName:          "timeleak.db",
		MaxOpenConns:    25,
		MaxIdleConns:    25,
		ConnMaxLifetime: 5 * time.Minute,
		JWT: JWTConfig{
			AccessSecret:   "change-me",
			AdminSecret:    "change-me-admin",
			AccessTTL:      60 * time.Second,
			AdminAccessTTL: 60 * time.Second,
			RefreshTTL:     30 * 24 * time.Hour,
			Issuer:         "time-leak",
		},
		OTP: OTPConfig{
			HMACSecret:      "change-me-otp",
			RequestCooldown: 30 * time.Second,
			MaxAttempts:     5,
			LockDuration:    2 * time.Minute,
			ExpiresIn:       5 * time.Minute,
		},
		Admin: AdminConfig{
			Username: "Admin",
			Password: "QRT123",
		},
		EnableTestingEndpoints: false,
	}

	if v := os.Getenv("APP_ADDR"); v != "" {
		cfg.Addr = strings.TrimSpace(v)
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = strings.TrimSpace(v)
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.DBName = strings.TrimSpace(v)
	}
	if v := os.Getenv("DB_MAX_OPEN_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("DB_MAX_OPEN_CONNS must be integer")
		}
		cfg.MaxOpenConns = n
	}
	if v := os.Getenv("DB_MAX_IDLE_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("DB_MAX_IDLE_CONNS must be integer")
		}
		cfg.MaxIdleConns = n
	}
	if v := os.Getenv("DB_CONN_MAX_LIFETIME_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("DB_CONN_MAX_LIFETIME_SEC must be integer")
		}
		cfg.ConnMaxLifetime = time.Duration(n) * time.Second
	}

	if v := os.Getenv("JWT_ACCESS_SECRET"); v != "" {
		cfg.JWT.AccessSecret = strings.TrimSpace(v)
	}
	if v := os.Getenv("JWT_ADMIN_SECRET"); v != "" {
		cfg.JWT.AdminSecret = strings.TrimSpace(v)
	}
	if v := os.Getenv("JWT_ISSUER"); v != "" {
		cfg.JWT.Issuer = strings.TrimSpace(v)
	}
	if v := os.Getenv("JWT_REFRESH_TTL_HOURS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("JWT_REFRESH_TTL_HOURS must be integer")
		}
		cfg.JWT.RefreshTTL = time.Duration(n) * time.Hour
	}

	if v := os.Getenv("OTP_HMAC_SECRET"); v != "" {
		cfg.OTP.HMACSecret = strings.TrimSpace(v)
	}
	if v := os.Getenv("OTP_REQUEST_COOLDOWN_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("OTP_REQUEST_COOLDOWN_SEC must be integer")
		}
		cfg.OTP.RequestCooldown = time.Duration(n) * time.Second
	}
	if v := os.Getenv("OTP_MAX_ATTEMPTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("OTP_MAX_ATTEMPTS must be integer")
		}
		cfg.OTP.MaxAttempts = n
	}
	if v := os.Getenv("OTP_LOCK_DURATION_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("OTP_LOCK_DURATION_SEC must be integer")
		}
		cfg.OTP.LockDuration = time.Duration(n) * time.Second
	}
	if v := os.Getenv("OTP_EXPIRES_IN_SEC"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, errors.New("OTP_EXPIRES_IN_SEC must be integer")
		}
		cfg.OTP.ExpiresIn = time.Duration(n) * time.Second
	}

	if v := os.Getenv("ADMIN_USERNAME"); v != "" {
		cfg.Admin.Username = strings.TrimSpace(v)
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" {
		cfg.Admin.Password = strings.TrimSpace(v)
	}
	if v := os.Getenv("ENABLE_TESTING_ENDPOINTS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, errors.New("ENABLE_TESTING_ENDPOINTS must be bool")
		}
		cfg.EnableTestingEndpoints = b
	}

	if cfg.DBPath == "" {
		return nil, errors.New("DB_PATH is empty")
	}
	if cfg.DBName == "" {
		return nil, errors.New("DB_NAME is empty")
	}
	if cfg.MaxOpenConns <= 0 {
		return nil, errors.New("DB_MAX_OPEN_CONNS must be > 0")
	}
	if cfg.MaxIdleConns < 0 {
		return nil, errors.New("DB_MAX_IDLE_CONNS must be >= 0")
	}
	if cfg.ConnMaxLifetime <= 0 {
		return nil, errors.New("DB_CONN_MAX_LIFETIME_SEC must be > 0")
	}
	if cfg.JWT.AccessSecret == "" {
		return nil, errors.New("JWT_ACCESS_SECRET is empty")
	}
	if cfg.JWT.AdminSecret == "" {
		return nil, errors.New("JWT_ADMIN_SECRET is empty")
	}
	if cfg.JWT.AccessTTL != 60*time.Second {
		return nil, errors.New("access token ttl must be exactly 60 seconds")
	}
	if cfg.OTP.ExpiresIn < 3*time.Minute || cfg.OTP.ExpiresIn > 5*time.Minute {
		return nil, errors.New("otp expiry must be between 180 and 300 seconds")
	}
	if cfg.OTP.MaxAttempts <= 0 {
		return nil, errors.New("OTP_MAX_ATTEMPTS must be > 0")
	}
	if cfg.OTP.RequestCooldown <= 0 {
		return nil, errors.New("OTP_REQUEST_COOLDOWN_SEC must be > 0")
	}
	if cfg.OTP.LockDuration <= 0 {
		return nil, errors.New("OTP_LOCK_DURATION_SEC must be > 0")
	}
	if cfg.Admin.Username == "" || cfg.Admin.Password == "" {
		return nil, errors.New("admin credentials are empty")
	}

	return cfg, nil
}

func (c *Config) GetDatabasePath() string {
	return filepath.Join(c.DBPath, c.DBName)
}
