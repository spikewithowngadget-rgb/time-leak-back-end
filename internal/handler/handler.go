package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/service"

	"go.uber.org/zap"
)

type Handler struct {
	app   service.IUserNotesService
	jwt   service.IJWTService
	otp   service.IOTPService
	ads   service.IAdsService
	admin service.IAdminAuthService
	cfg   *config.Config
	log   *zap.Logger
}

func New(services *service.Services, cfg *config.Config, log *zap.Logger) *Handler {
	if log == nil {
		log = zap.NewNop()
	}
	return &Handler{
		app:   services.App,
		jwt:   services.JWT,
		otp:   services.OTP,
		ads:   services.Ads,
		admin: services.Admin,
		cfg:   cfg,
		log:   log,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("GET /swagger", h.SwaggerUI)
	mux.HandleFunc("GET /swagger.json", h.SwaggerJSON)

	mux.HandleFunc("POST /api/v1/auth/login", h.AuthLogin)
	mux.HandleFunc("POST /api/v1/auth/otp/request", h.AuthOTPRequest)
	mux.HandleFunc("POST /api/v1/auth/otp/verify", h.AuthOTPVerify)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.AuthRefresh)
	mux.HandleFunc("GET /api/v1/auth/me", h.AuthMe)
	mux.HandleFunc("POST /api/v1/auth/notes", h.AuthCreateNote)
	mux.HandleFunc("GET /api/v1/auth/notes", h.AuthListNotes)

	mux.HandleFunc("GET /api/v1/me", h.Me)

	mux.HandleFunc("POST /api/v1/users/register", h.RegisterUser)
	mux.HandleFunc("POST /api/v1/users/login", h.Login)
	mux.HandleFunc("GET /api/v1/users/{id}", h.GetUser)
	mux.HandleFunc("PUT /api/v1/users/{id}/language", h.UpdateUserLanguage)

	mux.HandleFunc("POST /api/v1/notes", h.CreateNote)
	mux.HandleFunc("GET /api/v1/users/{id}/notes", h.ListNotes)

	mux.HandleFunc("GET /api/v1/ads/next", h.AdsNext)

	mux.HandleFunc("POST /api/v1/admin/auth/login", h.AdminLogin)
	mux.HandleFunc("POST /api/v1/admin/ads", h.AdminCreateAd)
	mux.HandleFunc("PUT /api/v1/admin/ads/{id}", h.AdminUpdateAd)
	mux.HandleFunc("DELETE /api/v1/admin/ads/{id}", h.AdminDeleteAd)
	mux.HandleFunc("GET /api/v1/admin/ads", h.AdminListAds)
	mux.HandleFunc("GET /api/v1/admin/testing/otp/latest", h.AdminLatestOTP)
}

type registerReq struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	UserLanguage string `json:"userLanguage"`
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authRefreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

type authCreateNoteReq struct {
	NoteType string `json:"note_type"`
}

type createNoteReq struct {
	UserID   string `json:"userId"`
	NoteType string `json:"note_type"`
}

type updateLanguageReq struct {
	UserLanguage string `json:"userLanguage"`
}

type otpRequestReq struct {
	Channel string `json:"channel"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
}

type otpVerifyReq struct {
	RequestID string `json:"request_id"`
	Code      string `json:"code"`
	Email     string `json:"email"`
}

type adminLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createAdReq struct {
	Title     string `json:"title"`
	ImageURL  string `json:"image_url"`
	TargetURL string `json:"target_url"`
	IsActive  *bool  `json:"is_active"`
}

type updateAdReq struct {
	Title     *string `json:"title"`
	ImageURL  *string `json:"image_url"`
	TargetURL *string `json:"target_url"`
	IsActive  *bool   `json:"is_active"`
}

func (h *Handler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	user, err := h.app.RegisterUser(r.Context(), req.Email, req.Password, req.UserLanguage)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailAlreadyExists):
			writeErrorJSON(w, http.StatusConflict, "email already exists")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	user, err := h.app.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	pair, err := h.jwt.IssueUserTokens(r.Context(), user, "password")
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user": user,
		"tokens": map[string]any{
			"access_token":       pair.AccessToken,
			"refresh_token":      pair.RefreshToken,
			"expires_in_seconds": h.jwt.AccessTTLSeconds(),
		},
	})
}

func (h *Handler) AuthLogin(w http.ResponseWriter, r *http.Request) {
	h.Login(w, r)
}

func (h *Handler) AuthOTPRequest(w http.ResponseWriter, r *http.Request) {
	var req otpRequestReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	channel := domain.OTPChannel(strings.ToLower(strings.TrimSpace(req.Channel)))
	destination := strings.TrimSpace(req.Email)
	if channel == domain.OTPChannelWhatsApp {
		destination = strings.TrimSpace(req.Phone)
	}

	result, err := h.otp.RequestOTP(r.Context(), channel, destination)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidOTPChannel), errors.Is(err, service.ErrInvalidOTPDestination):
			writeErrorJSON(w, http.StatusBadRequest, "invalid otp request")
		case errors.Is(err, service.ErrOTPTooManyRequests), errors.Is(err, service.ErrOTPLocked):
			writeErrorJSON(w, http.StatusTooManyRequests, "otp temporarily unavailable")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AuthOTPVerify(w http.ResponseWriter, r *http.Request) {
	var req otpVerifyReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	verifyResult, err := h.otp.VerifyOTP(r.Context(), req.RequestID, req.Code)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOTPRequestNotFound),
			errors.Is(err, service.ErrOTPExpired),
			errors.Is(err, service.ErrOTPAlreadyUsed),
			errors.Is(err, service.ErrOTPInvalidCode):
			writeErrorJSON(w, http.StatusBadRequest, "otp verification failed")
		case errors.Is(err, service.ErrOTPTooManyAttempts), errors.Is(err, service.ErrOTPLocked):
			writeErrorJSON(w, http.StatusTooManyRequests, "otp temporarily locked")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	user, err := h.app.ResolveUserForOTP(r.Context(), verifyResult.Channel, verifyResult.Destination, req.Email)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailRequiredForWhatsApp):
			writeErrorJSON(w, http.StatusBadRequest, "email is required for whatsapp otp")
		case errors.Is(err, service.ErrEmailAlreadyLinked):
			writeErrorJSON(w, http.StatusConflict, "email already linked to another account")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	authType := "otp_email"
	if verifyResult.Channel == domain.OTPChannelWhatsApp {
		authType = "otp_whatsapp"
	}
	pair, err := h.jwt.IssueUserTokens(r.Context(), user, authType)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":       pair.AccessToken,
		"refresh_token":      pair.RefreshToken,
		"expires_in_seconds": h.jwt.AccessTTLSeconds(),
		"user":               user,
	})
}

func (h *Handler) AuthRefresh(w http.ResponseWriter, r *http.Request) {
	var req authRefreshReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	pair, err := h.jwt.Refresh(r.Context(), strings.TrimSpace(req.RefreshToken))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRefreshNotFound),
			errors.Is(err, service.ErrRefreshRevoked),
			errors.Is(err, service.ErrRefreshExpired):
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":       pair.AccessToken,
		"refresh_token":      pair.RefreshToken,
		"expires_in_seconds": h.jwt.AccessTTLSeconds(),
	})
}

func (h *Handler) AuthMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_uuid": claims.UserUUID,
		"email":     claims.Email,
		"auth_type": claims.AuthType,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	user, err := h.app.GetUser(r.Context(), claims.UserUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) AuthCreateNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	var req authCreateNoteReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	note, err := h.app.CreateNote(r.Context(), claims.UserUUID, strings.TrimSpace(req.NoteType))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			writeErrorJSON(w, http.StatusNotFound, "user not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

func (h *Handler) AuthListNotes(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	notes, err := h.app.ListNotes(r.Context(), claims.UserUUID)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  notes,
		"total": len(notes),
	})
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	user, err := h.app.GetUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) UpdateUserLanguage(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	var req updateLanguageReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	if err := h.app.UpdateUserLanguage(r.Context(), userID, req.UserLanguage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	var req createNoteReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	note, err := h.app.CreateNote(r.Context(), strings.TrimSpace(req.UserID), strings.TrimSpace(req.NoteType))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			writeErrorJSON(w, http.StatusNotFound, "user not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	notes, err := h.app.ListNotes(r.Context(), userID)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  notes,
		"total": len(notes),
	})
}

func (h *Handler) AdsNext(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	ad, err := h.ads.NextAdForUser(r.Context(), claims.UserUUID)
	if err != nil {
		if errors.Is(err, service.ErrNoActiveAds) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, ad)
}

func (h *Handler) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var req adminLoginReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	result, err := h.admin.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidAdminCredentials) {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AdminCreateAd(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	var req createAdReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	ad, err := h.ads.CreateAd(r.Context(), domain.AdCreateInput{
		Title:     req.Title,
		ImageURL:  req.ImageURL,
		TargetURL: req.TargetURL,
		IsActive:  isActive,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidAdTitle), errors.Is(err, service.ErrInvalidAdURL):
			writeErrorJSON(w, http.StatusBadRequest, "invalid ad payload")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	writeJSON(w, http.StatusCreated, ad)
}

func (h *Handler) AdminUpdateAd(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	var req updateAdReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	ad, err := h.ads.UpdateAd(r.Context(), id, domain.AdUpdateInput{
		Title:     req.Title,
		ImageURL:  req.ImageURL,
		TargetURL: req.TargetURL,
		IsActive:  req.IsActive,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAdNotFound):
			writeErrorJSON(w, http.StatusNotFound, "not found")
		case errors.Is(err, service.ErrInvalidAdTitle), errors.Is(err, service.ErrInvalidAdURL):
			writeErrorJSON(w, http.StatusBadRequest, "invalid ad payload")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	writeJSON(w, http.StatusOK, ad)
}

func (h *Handler) AdminDeleteAd(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	if err := h.ads.DeleteAd(r.Context(), id); err != nil {
		if errors.Is(err, service.ErrAdNotFound) {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminListAds(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.adminClaimsFromRequest(w, r); !ok {
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "invalid offset")
			return
		}
		offset = parsed
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

	ads, err := h.ads.ListAds(r.Context(), limit, offset, active)
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  ads,
		"total": len(ads),
	})
}

func (h *Handler) AdminLatestOTP(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.EnableTestingEndpoints {
		writeErrorJSON(w, http.StatusNotFound, "not found")
		return
	}

	phone := strings.TrimSpace(r.URL.Query().Get("phone"))
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if (phone == "" && email == "") || (phone != "" && email != "") {
		writeErrorJSON(w, http.StatusBadRequest, "provide exactly one query param: phone or email")
		return
	}

	channel := domain.OTPChannelEmail
	destination := email
	if phone != "" {
		channel = domain.OTPChannelWhatsApp
		destination = phone
	}

	otpCode, err := h.otp.GetLatestTestingOTP(r.Context(), channel, destination)
	if err != nil {
		if errors.Is(err, service.ErrOTPTestingCodeNotFound) {
			writeErrorJSON(w, http.StatusNotFound, "otp not found")
			return
		}
		writeErrorJSON(w, http.StatusBadRequest, "invalid destination")
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(otpCode.Code))
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) userClaimsFromRequest(w http.ResponseWriter, r *http.Request) (*service.AccessClaims, bool) {
	access := bearerToken(r.Header.Get("Authorization"))
	if access == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}

	claims, err := h.jwt.VerifyUserAccess(access)
	if err != nil {
		if errors.Is(err, service.ErrForbiddenRole) {
			writeErrorJSON(w, http.StatusForbidden, "forbidden")
			return nil, false
		}
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}

	return claims, true
}

func (h *Handler) adminClaimsFromRequest(w http.ResponseWriter, r *http.Request) (*service.AccessClaims, bool) {
	access := bearerToken(r.Header.Get("Authorization"))
	if access == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}

	claims, err := h.jwt.VerifyAdminAccess(access)
	if err != nil {
		if errors.Is(err, service.ErrForbiddenRole) {
			writeErrorJSON(w, http.StatusForbidden, "forbidden")
			return nil, false
		}
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}

	return claims, true
}

func bearerToken(hdr string) string {
	if hdr == "" {
		return ""
	}
	parts := strings.SplitN(hdr, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func decodeJSONBody(r *http.Request, out any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func writeErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
