package domain

import "testing"

func TestNormalizeEmailAddress(t *testing.T) {
	for raw, want := range map[string]string{" User+tag@Example.COM ": "user+tag@example.com", "person@sub.timeleak.kz": "person@sub.timeleak.kz"} {
		got, ok := NormalizeEmailAddress(raw)
		if !ok || got != want {
			t.Errorf("unexpected normalization for %q", raw)
		}
	}
	for _, raw := range []string{"", "not-email", "user@localhost", "Name <user@example.com>", "one@example.com,two@example.com", "user@example.com\r\nBcc: other@example.com", "u@-example.com", "u@example..com", "u@example.com.", "u@[127.0.0.1]", "почта@example.com"} {
		if _, ok := NormalizeEmailAddress(raw); ok {
			t.Errorf("accepted invalid mailbox %q", raw)
		}
	}
}
