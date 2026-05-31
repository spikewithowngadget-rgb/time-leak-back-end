package service

import (
	"regexp"
	"strings"
)

var phoneE164Pattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}

	hasPlus := strings.HasPrefix(phone, "+")
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return ""
	}
	if hasPlus {
		return "+" + digits
	}
	switch {
	case len(digits) == 11 && strings.HasPrefix(digits, "8"):
		return "+7" + digits[1:]
	case len(digits) == 11 && strings.HasPrefix(digits, "7"):
		return "+" + digits
	case len(digits) == 10:
		return "+7" + digits
	default:
		return digits
	}
}

func validatePhoneStrict(phone string) (string, error) {
	normalizedPhone := normalizePhone(phone)
	if normalizedPhone == "" {
		return "", ErrPhoneRequired
	}
	if !phoneE164Pattern.MatchString(normalizedPhone) {
		return "", ErrInvalidPhoneFormat
	}
	return normalizedPhone, nil
}
