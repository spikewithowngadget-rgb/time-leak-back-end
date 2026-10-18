package domain

import (
	"fmt"
	"net/mail"
	"strings"
)

// NormalizeEmailAddress accepts one plain Internet mailbox, not a display name
// or an address list. Canonicalization also prevents case-based quota bypasses.
func NormalizeEmailAddress(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 254 || strings.ContainsAny(value, "\r\n\t") {
		return "", false
	}
	for _, c := range value {
		if c > 127 {
			return "", false
		}
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Name != "" || parsed.Address != value {
		return "", false
	}
	at := strings.LastIndexByte(value, '@')
	if at < 1 || at > 64 {
		return "", false
	}
	labels := strings.Split(value[at+1:], ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}
	return value, true
}

// EmailOTPRequestLimitError carries a server-calculated retry delay.
type EmailOTPRequestLimitError struct {
	RetryAfterSeconds int
	Daily             bool
}

func (e *EmailOTPRequestLimitError) Error() string {
	return fmt.Sprintf("email otp request limit reached; retry after %d seconds", e.RetryAfterSeconds)
}

type EmailOTPRequestResult struct {
	OTPRequestResult
	Email              string `json:"email"`
	ResendAfterSeconds int    `json:"resend_after_seconds"`
}

type EmailOTPVerifyResult struct {
	RequestID string `json:"request_id"`
	Email     string `json:"email"`
	Verified  bool   `json:"verified"`
}
