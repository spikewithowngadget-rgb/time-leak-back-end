package config

import (
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type SharingConfig struct {
	PublicBaseURL                 string
	SigningSecret                 string
	FFmpegPath                    string
	FFprobePath                   string
	AppleAppID                    string
	AppleAppStoreID               string
	AndroidPackageName            string
	AndroidSHA256CertFingerprints []string
}

func loadSharingConfig() (SharingConfig, error) {
	get := func(key, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	cfg := SharingConfig{
		PublicBaseURL: strings.TrimRight(get("PUBLIC_BASE_URL", "https://timeleak.kz"), "/"),
		SigningSecret: get("SHARE_SIGNING_SECRET", ""),
		FFmpegPath:    get("NOTE_FFMPEG_PATH", "ffmpeg"), FFprobePath: get("NOTE_FFPROBE_PATH", "ffprobe"),
		AppleAppID:         get("APPLE_APP_ID", "9YQXS629VS.kz.timeleak.project.app"),
		AppleAppStoreID:    get("APPLE_APP_STORE_ID", "6762095094"),
		AndroidPackageName: get("ANDROID_PACKAGE_NAME", "kz.timeleak.project.app"),
	}
	u, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return cfg, errors.New("PUBLIC_BASE_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if !regexp.MustCompile(`^[A-Z0-9]{10}\.[A-Za-z0-9][A-Za-z0-9.-]+$`).MatchString(cfg.AppleAppID) {
		return cfg, errors.New("APPLE_APP_ID must contain the app ID prefix and bundle ID")
	}
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(cfg.AppleAppStoreID) {
		return cfg, errors.New("APPLE_APP_STORE_ID must be a numeric App Store ID")
	}
	if !regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`).MatchString(cfg.AndroidPackageName) {
		return cfg, errors.New("ANDROID_PACKAGE_NAME must be an Android application ID")
	}
	fingerprints := strings.TrimSpace(os.Getenv("ANDROID_SHA256_CERT_FINGERPRINTS"))
	if fingerprints != "" {
		for _, value := range strings.Split(fingerprints, ",") {
			fingerprint := strings.ToUpper(strings.TrimSpace(value))
			if !regexp.MustCompile(`^([0-9A-F]{2}:){31}[0-9A-F]{2}$`).MatchString(fingerprint) {
				return cfg, errors.New("ANDROID_SHA256_CERT_FINGERPRINTS must contain comma-separated SHA256 fingerprints (32 colon-separated hex bytes each)")
			}
			cfg.AndroidSHA256CertFingerprints = append(cfg.AndroidSHA256CertFingerprints, fingerprint)
		}
	}
	return cfg, nil
}

func (c SharingConfig) AppStoreURL() string {
	return "https://apps.apple.com/app/id" + c.AppleAppStoreID
}

func (c SharingConfig) PlayStoreURL() string {
	return "https://play.google.com/store/apps/details?id=" + c.AndroidPackageName
}
