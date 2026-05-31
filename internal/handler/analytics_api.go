package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"time-leak/internal/domain"
)

// adminDeviceResponse is the masked device shape returned to admins.
type adminDeviceResponse struct {
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

func toAdminDeviceResponse(device domain.UserDevice) adminDeviceResponse {
	return adminDeviceResponse{
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
	}
}

// AdminAnalyticsOverview returns aggregate counts for ads, devices and locations.
func (h *Handler) AdminAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	overview, err := h.analytics.Overview(r.Context())
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, overview)
}

// AdminAnalyticsGeo returns map points, approximate region grouping and a summary.
func (h *Handler) AdminAnalyticsGeo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	from, ok := parseOptionalTimeQuery(r, w, "from")
	if !ok {
		return
	}
	to, ok := parseOptionalTimeQuery(r, w, "to")
	if !ok {
		return
	}

	platform, ok := parsePlatformQuery(r, w)
	if !ok {
		return
	}
	source, ok := parseSourceQuery(r, w)
	if !ok {
		return
	}

	limit := 1000
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	activeOnly := false
	if raw := strings.TrimSpace(r.URL.Query().Get("active")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid active")
			return
		}
		activeOnly = parsed
	}

	result, err := h.analytics.Geo(r.Context(), domain.GeoFilter{
		From:       from,
		To:         to,
		Platform:   platform,
		Source:     source,
		ActiveOnly: activeOnly,
		Limit:      limit,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// AdminListDevices returns devices across all users, with masked phones and a
// real total count for the applied filter.
func (h *Handler) AdminListDevices(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	limit, offset, ok := parsePagination(r, w)
	if !ok {
		return
	}

	platform, ok := parsePlatformQuery(r, w)
	if !ok {
		return
	}

	var active *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("active")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid active")
			return
		}
		active = &parsed
	}

	devices, total, err := h.analytics.ListDevices(r.Context(), domain.AdminDeviceListFilter{
		Platform: platform,
		Active:   active,
		Search:   strings.TrimSpace(r.URL.Query().Get("search")),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	out := make([]adminDeviceResponse, 0, len(devices))
	for _, device := range devices {
		out = append(out, toAdminDeviceResponse(device))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  out,
		"total": total,
	})
}

func parsePlatformQuery(r *http.Request, w http.ResponseWriter) (string, bool) {
	raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("platform")))
	switch raw {
	case "", "all":
		return "", true
	case "ios", "android":
		return raw, true
	default:
		writeErrorJSON(w, http.StatusBadRequest, "invalid platform")
		return "", false
	}
}

func parseSourceQuery(r *http.Request, w http.ResponseWriter) (string, bool) {
	raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("source")))
	switch raw {
	case "", "all":
		return "", true
	case "gps", "network", "manual", "unknown":
		return raw, true
	default:
		writeErrorJSON(w, http.StatusBadRequest, "invalid source")
		return "", false
	}
}
