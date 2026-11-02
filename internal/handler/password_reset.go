package handler

import (
	"errors"
	"net/http"

	"time-leak/internal/service"

	"go.uber.org/zap"
)

func (h *Handler) passwordResetError(w http.ResponseWriter, err error) {
	var code, message string
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, service.ErrPhoneRequired):
		code, message = "phone_required", service.ErrPhoneRequired.Error()
	case errors.Is(err, service.ErrInvalidPhoneFormat):
		code, message = "invalid_phone", service.ErrInvalidPhoneFormat.Error()
	case errors.Is(err, service.ErrPasswordRequired):
		code, message = "password_required", service.ErrPasswordRequired.Error()
	case errors.Is(err, service.ErrPasswordMismatch):
		code, message = "password_mismatch", service.ErrPasswordMismatch.Error()
	case errors.Is(err, service.ErrPasswordTooShort):
		code, message = "password_too_short", service.ErrPasswordTooShort.Error()
	case errors.Is(err, service.ErrPasswordTooLong):
		code, message = "password_too_long", service.ErrPasswordTooLong.Error()
	case errors.Is(err, service.ErrVerificationTokenMissing):
		code, message = "verification_token_missing", service.ErrVerificationTokenMissing.Error()
	case errors.Is(err, service.ErrVerificationNotFound):
		code, message = "verification_token_not_found", service.ErrVerificationNotFound.Error()
	case errors.Is(err, service.ErrVerificationExpired):
		code, message = "verification_token_expired", service.ErrVerificationExpired.Error()
	case errors.Is(err, service.ErrVerificationAlreadyUsed):
		code, message = "verification_token_used", service.ErrVerificationAlreadyUsed.Error()
	case errors.Is(err, service.ErrVerificationInvalid):
		code, message = "verification_token_invalid", service.ErrVerificationInvalid.Error()
	case errors.Is(err, service.ErrUserNotFound):
		code, message, status = "user_not_found", service.ErrUserNotFound.Error(), http.StatusNotFound
	default:
		h.log.Error("password reset failed",
			zap.String("reason", "internal_error"),
			zap.Int("status", http.StatusInternalServerError),
			zap.Error(err),
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal", "code": "internal_error"})
		return
	}
	h.writePasswordResetFailure(w, code, message, status)
}

func (h *Handler) writePasswordResetFailure(w http.ResponseWriter, code, message string, status int) {
	h.log.Warn("password reset rejected", zap.String("reason", code), zap.Int("status", status))
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}
