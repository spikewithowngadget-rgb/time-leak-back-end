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

	var b strings.Builder
	for i, r := range phone {
		if i == 0 && r == '+' {
			b.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
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
