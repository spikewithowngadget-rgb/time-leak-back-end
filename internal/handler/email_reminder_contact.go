package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"time-leak/internal/domain"
	"time-leak/internal/service"
)

type emailReminderContacts interface {
	GetEmailReminderContact(context.Context, string) (domain.EmailReminderContact, error)
	UpsertVerifiedEmailReminderContact(context.Context, string, string, bool, string) (domain.EmailReminderContact, error)
	SetEmailReminderContactEnabled(context.Context, string, bool, *string) (domain.EmailReminderContact, error)
}

func writeEmailReminderContact(w http.ResponseWriter, contact domain.EmailReminderContact) {
	writeJSON(w, http.StatusOK, map[string]any{"email": contact.Email, "verified_at": contact.VerifiedAt, "enabled": contact.Enabled, "language": contact.Language})
}

func writeEmailReminderContactError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrUserInactive):
		writeErrorJSON(w, http.StatusForbidden, "account_deactivated")
	case errors.Is(err, service.ErrEmailReminderNotVerified), errors.Is(err, service.ErrEmailReminderLanguage):
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, sql.ErrNoRows):
		writeErrorJSON(w, http.StatusNotFound, "not found")
	default:
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
	}
}

func (h *Handler) GetEmailReminderContact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	app, ok := h.app.(emailReminderContacts)
	if !ok {
		writeErrorJSON(w, 503, "email reminders unavailable")
		return
	}
	contact, err := app.GetEmailReminderContact(r.Context(), claims.UserUUID)
	if err != nil {
		writeEmailReminderContactError(w, err)
		return
	}
	writeEmailReminderContact(w, contact)
}

func (h *Handler) VerifyEmailReminderContact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
		Code      string `json:"code"`
		Enabled   bool   `json:"enabled"`
		Language  string `json:"language"`
	}
	if err := decodeEmailOTPBody(w, r, &req); err != nil {
		writeErrorJSON(w, 400, "invalid email reminder verification request")
		return
	}
	language, err := service.NormalizeEmailReminderLanguage(req.Language)
	if err != nil {
		writeEmailReminderContactError(w, err)
		return
	}
	app, ok := h.app.(emailReminderContacts)
	if !ok || h.emailOTP == nil {
		writeErrorJSON(w, 503, "email reminders unavailable")
		return
	}
	if _, err := app.GetEmailReminderContact(r.Context(), claims.UserUUID); err != nil {
		writeEmailReminderContactError(w, err)
		return
	}
	verified, err := h.emailOTP.Verify(r.Context(), req.RequestID, req.Code)
	if err != nil {
		h.writeEmailOTPError(w, err, true)
		return
	}
	if !verified.Verified {
		writeErrorJSON(w, 400, "email otp verification failed")
		return
	}
	contact, err := app.UpsertVerifiedEmailReminderContact(r.Context(), claims.UserUUID, verified.Email, req.Enabled, language)
	if err != nil {
		writeEmailReminderContactError(w, err)
		return
	}
	writeEmailReminderContact(w, contact)
}

func (h *Handler) UpdateEmailReminderContact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled  *bool   `json:"enabled"`
		Language *string `json:"language"`
	}
	if err := decodeEmailOTPBody(w, r, &req); err != nil || req.Enabled == nil {
		writeErrorJSON(w, 400, "enabled must be a boolean")
		return
	}
	app, ok := h.app.(emailReminderContacts)
	if !ok {
		writeErrorJSON(w, 503, "email reminders unavailable")
		return
	}
	contact, err := app.SetEmailReminderContactEnabled(r.Context(), claims.UserUUID, *req.Enabled, req.Language)
	if err != nil {
		writeEmailReminderContactError(w, err)
		return
	}
	writeEmailReminderContact(w, contact)
}
