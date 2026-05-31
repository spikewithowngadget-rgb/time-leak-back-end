package handler

import (
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"time-leak/internal/domain"
	"time-leak/internal/service"
)

func (h *Handler) AdminListUserDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	devices, err := h.app.ListUserDevices(r.Context(), userID)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	type deviceResponse struct {
		ID           string    `json:"id"`
		UserID       string    `json:"user_id"`
		PhoneMasked  string    `json:"phone_masked"`
		DeviceID     string    `json:"device_id"`
		Platform     string    `json:"platform"`
		AppVersion   string    `json:"app_version,omitempty"`
		OSVersion    string    `json:"os_version,omitempty"`
		DeviceModel  string    `json:"device_model,omitempty"`
		Manufacturer string    `json:"manufacturer,omitempty"`
		HasPushToken bool      `json:"has_push_token"`
		FirstSeenAt  time.Time `json:"first_seen_at"`
		LastSeenAt   time.Time `json:"last_seen_at"`
		IsActive     bool      `json:"is_active"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
	}

	out := make([]deviceResponse, 0, len(devices))
	for _, device := range devices {
		out = append(out, deviceResponse{
			ID:           device.ID,
			UserID:       device.UserID,
			PhoneMasked:  maskPhone(device.Phone),
			DeviceID:     device.DeviceID,
			Platform:     device.Platform,
			AppVersion:   device.AppVersion,
			OSVersion:    device.OSVersion,
			DeviceModel:  device.DeviceModel,
			Manufacturer: device.Manufacturer,
			HasPushToken: strings.TrimSpace(device.PushToken) != "",
			FirstSeenAt:  device.FirstSeenAt,
			LastSeenAt:   device.LastSeenAt,
			IsActive:     device.IsActive,
			CreatedAt:    device.CreatedAt,
			UpdatedAt:    device.UpdatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  out,
		"total": len(out),
	})
}

func (h *Handler) AdminDeactivateUserDevice(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	userID := strings.TrimSpace(r.PathValue("id"))
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if userID == "" || deviceID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	if err := h.app.DeactivateUserDevice(r.Context(), userID, deviceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminListUserLocations(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	filter, ok := parseLocationFilter(r, userID, w)
	if !ok {
		return
	}

	events, err := h.app.ListUserLocationEvents(r.Context(), filter)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  events,
		"total": len(events),
	})
}

func (h *Handler) AdminListAuthEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	filter, ok := parseAuthEventFilter(r, w)
	if !ok {
		return
	}

	events, err := h.app.ListAuthEvents(r.Context(), filter)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	type authEventResponse struct {
		ID           string    `json:"id"`
		UserID       string    `json:"user_id,omitempty"`
		PhoneMasked  string    `json:"phone_masked,omitempty"`
		EventType    string    `json:"event_type"`
		DeviceID     string    `json:"device_id,omitempty"`
		IPAddress    string    `json:"ip_address,omitempty"`
		UserAgent    string    `json:"user_agent,omitempty"`
		MetadataJSON string    `json:"metadata_json,omitempty"`
		CreatedAt    time.Time `json:"created_at"`
	}
	out := make([]authEventResponse, 0, len(events))
	for _, event := range events {
		out = append(out, authEventResponse{
			ID:           event.ID,
			UserID:       event.UserID,
			PhoneMasked:  maskPhone(event.Phone),
			EventType:    event.EventType,
			DeviceID:     event.DeviceID,
			IPAddress:    event.IPAddress,
			UserAgent:    event.UserAgent,
			MetadataJSON: event.MetadataJSON,
			CreatedAt:    event.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  out,
		"total": len(out),
	})
}

func requestAuthContext(r *http.Request) service.AuthRequestContext {
	return service.AuthRequestContext{
		IPAddress: clientIPAddress(r),
		UserAgent: strings.TrimSpace(r.UserAgent()),
	}
}

func clientIPAddress(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func parsePagination(r *http.Request, w http.ResponseWriter) (int, int, bool) {
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		limit = parsed
	}
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid offset")
			return 0, 0, false
		}
		offset = parsed
	}
	return limit, offset, true
}

func parseLocationFilter(r *http.Request, userID string, w http.ResponseWriter) (domain.UserLocationListFilter, bool) {
	limit, offset, ok := parsePagination(r, w)
	if !ok {
		return domain.UserLocationListFilter{}, false
	}
	from, ok := parseOptionalTimeQuery(r, w, "from")
	if !ok {
		return domain.UserLocationListFilter{}, false
	}
	to, ok := parseOptionalTimeQuery(r, w, "to")
	if !ok {
		return domain.UserLocationListFilter{}, false
	}
	return domain.UserLocationListFilter{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
		From:   from,
		To:     to,
	}, true
}

func parseAuthEventFilter(r *http.Request, w http.ResponseWriter) (domain.AuthEventListFilter, bool) {
	limit, offset, ok := parsePagination(r, w)
	if !ok {
		return domain.AuthEventListFilter{}, false
	}
	from, ok := parseOptionalTimeQuery(r, w, "from")
	if !ok {
		return domain.AuthEventListFilter{}, false
	}
	to, ok := parseOptionalTimeQuery(r, w, "to")
	if !ok {
		return domain.AuthEventListFilter{}, false
	}
	return domain.AuthEventListFilter{
		Phone:     strings.TrimSpace(r.URL.Query().Get("phone")),
		UserID:    strings.TrimSpace(r.URL.Query().Get("user_id")),
		EventType: strings.TrimSpace(r.URL.Query().Get("event_type")),
		Limit:     limit,
		Offset:    offset,
		From:      from,
		To:        to,
	}, true
}

func parseOptionalTimeQuery(r *http.Request, w http.ResponseWriter, key string) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid "+key)
		return nil, false
	}
	return &parsed, true
}

func maskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) <= 4 {
		return phone
	}
	runes := []rune(phone)
	for i := 2; i < len(runes)-2; i++ {
		if runes[i] >= '0' && runes[i] <= '9' {
			runes[i] = '*'
		}
	}
	return string(runes)
}
