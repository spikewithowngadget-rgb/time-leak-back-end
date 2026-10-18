package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type SharingConfig struct {
	PublicBaseURL string
	SigningSecret string
	FFmpegPath    string
	FFprobePath   string
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
	}
	u, err := url.Parse(cfg.PublicBaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return cfg, errors.New("PUBLIC_BASE_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	return cfg, nil
}
