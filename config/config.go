package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type JWTConfig struct {
	AccessSecret string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	Issuer       string
}

type Config struct {
	Addr            string
	DBPath          string
	DBName          string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	JWT             JWTConfig
}

func NewConfig() (*Config, error) {
	cfg := new(Config)

	cfg.Addr = ":8081"
	cfg.DBPath = "data"
	cfg.DBName = "timeleak.db"
	cfg.MaxOpenConns = 25
	cfg.MaxIdleConns = 25
	cfg.ConnMaxLifetime = 5 * time.Minute

	// JWT defaults (можешь менять под себя)
	cfg.JWT = JWTConfig{
		AccessSecret: "change-me",         // лучше через ENV
		AccessTTL:    60 * time.Second,    // access token TTL
		RefreshTTL:   30 * 24 * time.Hour, // refresh token TTL
		Issuer:       "tax-bot",
	}

	// optional: override from env (если есть)
	if v := os.Getenv("JWT_ACCESS_SECRET"); v != "" {
		cfg.JWT.AccessSecret = v
	}
	if v := os.Getenv("JWT_ISSUER"); v != "" {
		cfg.JWT.Issuer = v
	}
	if v := os.Getenv("APP_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.DBName = v
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

	// minimal validation
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

	return cfg, nil
}

func (c *Config) GetDatabasePath() string {
	return filepath.Join(c.DBPath, c.DBName)
}
