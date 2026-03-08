package handler

import (
	"database/sql"
	"errors"
	"net/http"
)

func (h *Handler) AdminTestingAccessToken(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.EnableTestingEndpoints {
		writeErrorJSON(w, http.StatusNotFound, "not found")
		return
	}

	var req testingAccessTokenReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	user, err := h.app.GetUserByPhone(r.Context(), req.Phone)
	if err != nil {
		switch {
		case isAuthValidationError(err):
			writeErrorJSON(w, http.StatusBadRequest, "bad request")
		case errors.Is(err, sql.ErrNoRows):
			writeErrorJSON(w, http.StatusNotFound, "not found")
		default:
			writeErrorJSON(w, http.StatusInternalServerError, "internal")
		}
		return
	}

	accessToken, err := h.jwt.IssueTestingUserAccessToken(user, "testing_phone_forever")
	if err != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"userId":       user.UserID,
		"phone":        user.Phone,
		"auth_type":    "testing_phone_forever",
	})
}
