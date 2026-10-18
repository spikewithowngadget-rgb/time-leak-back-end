package config

import (
	"testing"
	"time"
)

func TestResendConfig_Environment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	t.Setenv("RESEND_API_KEY", "test-key")
	t.Setenv("RESEND_FROM_EMAIL", "noreply@timeleak.kz")
	t.Setenv("RESEND_FROM_NAME", "Time Leak")
	t.Setenv("OTP_RESEND_COOLDOWN", "32s")
	t.Setenv("RATE_OTP_REQUEST_PER_DAY", "10")
	t.Setenv("OTP_REQUEST_COOLDOWN_SEC", "30")
	cfg, err := NewConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resend.APIKey != "test-key" || cfg.Resend.FromEmail != "noreply@timeleak.kz" || cfg.Resend.FromName != "Time Leak" || cfg.Resend.BaseURL != "https://api.resend.com" {
		t.Fatal("Resend configuration was not loaded")
	}
	if cfg.EmailOTP.RequestCooldown != 32*time.Second || cfg.EmailOTP.MaxRequestsPerDay != 10 {
		t.Fatal("email OTP limits were not loaded")
	}
	if cfg.OTP.RequestCooldown != 30*time.Second {
		t.Fatal("email configuration changed WhatsApp cooldown")
	}
}

func TestResendConfig_RejectsInvalidLimits(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_ENV", "development")
	for _, tc := range []struct{ key, value string }{{"OTP_RESEND_COOLDOWN", "oops"}, {"OTP_RESEND_COOLDOWN", "0s"}, {"OTP_RESEND_COOLDOWN", "-1s"}, {"RATE_OTP_REQUEST_PER_DAY", "0"}, {"RATE_OTP_REQUEST_PER_DAY", "-2"}, {"RATE_OTP_REQUEST_PER_DAY", "oops"}, {"RESEND_FROM_EMAIL", "Name <user@example.com>"}, {"RESEND_FROM_NAME", "Name\r\nHeader"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := NewConfig(); err == nil {
				t.Fatal("expected invalid email configuration to fail")
			}
		})
	}
}
