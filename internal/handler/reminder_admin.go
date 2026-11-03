package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"time-leak/internal/domain"
)

func (h *Handler) AdminGetReminderSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}
	if h.reminders == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "reminders unavailable")
		return
	}
	settings, err := h.reminders.GetReminderSettings(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "unable to load reminder settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) AdminUpdateReminderSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}
	if h.reminders == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "reminders unavailable")
		return
	}
	var input struct {
		Enabled    *bool   `json:"enabled"`
		DaysBefore *int    `json:"days_before"`
		SendHour   *int    `json:"send_hour"`
		Timezone   *string `json:"timezone"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid reminder settings")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeErrorJSON(w, http.StatusBadRequest, "expected one settings object")
		return
	}
	if input.Enabled == nil || input.DaysBefore == nil || input.SendHour == nil || input.Timezone == nil {
		writeErrorJSON(w, http.StatusBadRequest, "enabled, days_before, send_hour and timezone are required")
		return
	}
	zone := strings.TrimSpace(*input.Timezone)
	proposed := domain.ReminderSettings{Enabled: *input.Enabled, DaysBefore: *input.DaysBefore, SendHour: *input.SendHour, Timezone: zone}
	if len(zone) > 100 || domain.ValidateReminderSettings(proposed) != nil {
		writeErrorJSON(w, http.StatusBadRequest, domain.ErrInvalidReminderSettings.Error())
		return
	}
	settings, err := h.reminders.UpdateReminderSettings(r.Context(), proposed)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidReminderSettings) {
			writeErrorJSON(w, http.StatusBadRequest, domain.ErrInvalidReminderSettings.Error())
		} else {
			writeErrorJSON(w, http.StatusInternalServerError, "unable to save reminder settings")
		}
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) AdminReminderStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}
	if h.reminders == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "reminders unavailable")
		return
	}
	status, err := h.reminders.ReminderStatus(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "unable to load reminder status")
		return
	}
	writeJSON(w, http.StatusOK, status)
}
