package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time-leak/internal/service"
)

type Handler struct {
	app service.IUserNotesService
	jwt service.IJWTService
}

func New(app service.IUserNotesService, jwt service.IJWTService) *Handler {
	return &Handler{app: app, jwt: jwt}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.Health)
	mux.HandleFunc("GET /swagger", h.SwaggerUI)
	mux.HandleFunc("GET /swagger.json", h.SwaggerJSON)

	mux.HandleFunc("POST /api/v1/auth/login", h.AuthLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.AuthRefresh)
	mux.HandleFunc("GET /api/v1/auth/me", h.AuthMe)
	mux.HandleFunc("POST /api/v1/auth/notes", h.AuthCreateNote)
	mux.HandleFunc("GET /api/v1/auth/notes", h.AuthListNotes)

	mux.HandleFunc("POST /api/v1/users/register", h.RegisterUser)
	mux.HandleFunc("POST /api/v1/users/login", h.Login)
	mux.HandleFunc("GET /api/v1/users/{id}", h.GetUser)
	mux.HandleFunc("PUT /api/v1/users/{id}/language", h.UpdateUserLanguage)

	mux.HandleFunc("POST /api/v1/notes", h.CreateNote)
	mux.HandleFunc("GET /api/v1/users/{id}/notes", h.ListNotes)
}

type registerReq struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	UserLanguage string `json:"userLanguage"`
}

func (h *Handler) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := h.app.RegisterUser(req.Email, req.Password, req.UserLanguage)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmailAlreadyExists):
			http.Error(w, "email already exists", http.StatusConflict)
		default:
			http.Error(w, "internal", http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusCreated, user)
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

func (h *Handler) AuthLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := h.app.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	pair, err := h.jwt.IssueTokensByEmail(user.Email)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user":   user,
		"tokens": pair,
	})
}

func (h *Handler) AuthRefresh(w http.ResponseWriter, r *http.Request) {
	var req authRefreshReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	pair, err := h.jwt.Refresh(strings.TrimSpace(req.RefreshToken))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRefreshNotFound), errors.Is(err, service.ErrRefreshRevoked), errors.Is(err, service.ErrRefreshExpired):
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			http.Error(w, "internal", http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, pair)
}

func (h *Handler) AuthMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authClaimsFromRequest(w, r)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"user_uuid": claims.UserUUID,
		"email":     claims.Email,
	})
}

func (h *Handler) AuthCreateNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authClaimsFromRequest(w, r)
	if !ok {
		return
	}

	var req authCreateNoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	note, err := h.app.CreateNote(claims.UserUUID, strings.TrimSpace(req.NoteType))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

func (h *Handler) AuthListNotes(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authClaimsFromRequest(w, r)
	if !ok {
		return
	}

	notes, err := h.app.ListNotes(claims.UserUUID)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  notes,
		"total": len(notes),
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := h.app.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, user)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := h.app.GetUser(userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, user)
}

type updateLanguageReq struct {
	UserLanguage string `json:"userLanguage"`
}

func (h *Handler) UpdateUserLanguage(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var req updateLanguageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := h.app.UpdateUserLanguage(userID, req.UserLanguage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createNoteReq struct {
	UserID   string `json:"userId"`
	NoteType string `json:"note_type"`
}

func (h *Handler) CreateNote(w http.ResponseWriter, r *http.Request) {
	var req createNoteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	note, err := h.app.CreateNote(strings.TrimSpace(req.UserID), strings.TrimSpace(req.NoteType))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, note)
}

func (h *Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if userID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	notes, err := h.app.ListNotes(userID)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  notes,
		"total": len(notes),
	})
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) authClaimsFromRequest(w http.ResponseWriter, r *http.Request) (*service.AccessClaims, bool) {
	access := bearerToken(r.Header.Get("Authorization"))
	if access == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, false
	}

	claims, err := h.jwt.VerifyAccess(access)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
