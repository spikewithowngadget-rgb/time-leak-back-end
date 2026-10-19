package config

import (
	"strings"
	"testing"
)

const testAndroidFingerprint = "D2:62:FF:B6:A9:02:AC:48:EA:00:1B:B7:2A:89:A0:E3:31:8B:39:07:E8:05:92:B7:BF:F3:5C:A8:D6:DD:A0:19"

func TestSharingConfigAppLinks(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://timeleak.kz/")
	t.Setenv("APPLE_APP_ID", "9YQXS629VS.kz.timeleak.project.app")
	t.Setenv("APPLE_APP_STORE_ID", "6762095094")
	t.Setenv("ANDROID_PACKAGE_NAME", "kz.timeleak.project.app")
	t.Setenv("ANDROID_SHA256_CERT_FINGERPRINTS", strings.ToLower(testAndroidFingerprint)+", "+strings.Repeat("AA:", 31)+"AA")
	cfg, err := loadSharingConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicBaseURL != "https://timeleak.kz" || cfg.AppleAppID != "9YQXS629VS.kz.timeleak.project.app" {
		t.Fatalf("unexpected link configuration: %+v", cfg)
	}
	if len(cfg.AndroidSHA256CertFingerprints) != 2 || cfg.AndroidSHA256CertFingerprints[0] != testAndroidFingerprint {
		t.Fatalf("fingerprints were not normalized: %v", cfg.AndroidSHA256CertFingerprints)
	}
	if cfg.AppStoreURL() != "https://apps.apple.com/app/id6762095094" || cfg.PlayStoreURL() != "https://play.google.com/store/apps/details?id=kz.timeleak.project.app" {
		t.Fatal("incorrect store URLs")
	}
}

func TestSharingConfigRejectsInvalidAppLinks(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"APPLE_APP_ID", "kz.timeleak.project.app"},
		{"APPLE_APP_STORE_ID", "9YQXS629VS"},
		{"ANDROID_PACKAGE_NAME", "kz.timeleak/app"},
		{"ANDROID_SHA256_CERT_FINGERPRINTS", "<SHA256>"},
		{"ANDROID_SHA256_CERT_FINGERPRINTS", strings.Repeat("AA:", 30) + "AA"},
		{"ANDROID_SHA256_CERT_FINGERPRINTS", strings.Repeat("GG:", 31) + "GG"},
		{"ANDROID_SHA256_CERT_FINGERPRINTS", testAndroidFingerprint + ","},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := loadSharingConfig(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected validation error for %s, got %v", tc.key, err)
			}
		})
	}
}
