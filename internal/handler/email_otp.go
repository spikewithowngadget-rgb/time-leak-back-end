package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"time-leak/internal/domain"
	"time-leak/internal/service"
)

func (h *Handler) AuthEmailOTPRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeEmailOTPBody(w, r, &req); err != nil {
		writeErrorJSON(w, 400, "invalid email otp request")
		return
	}
	if h.emailOTP == nil {
		writeErrorJSON(w, 503, "email otp delivery unavailable")
		return
	}
	result, err := h.emailOTP.Request(r.Context(), req.Email)
	if err != nil {
		h.writeEmailOTPError(w, err, false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) AuthEmailOTPVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var req otpVerifyReq
	if err := decodeEmailOTPBody(w, r, &req); err != nil {
		writeErrorJSON(w, 400, "invalid email otp request")
		return
	}
	if h.emailOTP == nil {
		writeErrorJSON(w, 503, "email otp delivery unavailable")
		return
	}
	result, err := h.emailOTP.Verify(r.Context(), req.RequestID, req.Code)
	if err != nil {
		h.writeEmailOTPError(w, err, true)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeEmailOTPBody(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

func (h *Handler) writeEmailOTPError(w http.ResponseWriter, err error, verifying bool) {
	var rateErr *domain.EmailOTPRequestLimitError
	switch {
	case errors.As(err, &rateErr):
		message := "email otp request is too frequent"
		if rateErr.Daily {
			message = "email otp daily request limit reached"
		}
		writeEmailOTPRateError(w, message, rateErr.RetryAfterSeconds)
	case errors.Is(err, service.ErrOTPTooManyRequests):
		cooldown := h.cfg.EmailOTP.RequestCooldown
		if cooldown <= 0 {
			cooldown = 32 * time.Second
		}
		writeEmailOTPRateError(w, "email otp request is too frequent", int(math.Ceil(cooldown.Seconds())))
	case errors.Is(err, service.ErrOTPTooManyAttempts), errors.Is(err, service.ErrOTPLocked):
		lock := h.cfg.OTP.LockDuration
		if lock <= 0 {
			lock = 2 * time.Minute
		}
		writeEmailOTPRateError(w, "email otp temporarily locked", int(math.Ceil(lock.Seconds())))
	case errors.Is(err, service.ErrInvalidOTPDestination):
		writeErrorJSON(w, 400, "invalid email address")
	case errors.Is(err, service.ErrOTPDeliveryUnavailable):
		writeErrorJSON(w, 503, "email otp delivery unavailable")
	case errors.Is(err, service.ErrOTPDeliveryFailed):
		writeErrorJSON(w, 502, "email otp delivery failed")
	case errors.Is(err, service.ErrOTPRequestNotFound), errors.Is(err, service.ErrOTPExpired),
		errors.Is(err, service.ErrOTPAlreadyUsed), errors.Is(err, service.ErrOTPInvalidCode), errors.Is(err, sql.ErrNoRows):
		if verifying {
			writeErrorJSON(w, 400, "email otp verification failed")
		} else {
			writeErrorJSON(w, 500, "internal")
		}
	default:
		writeErrorJSON(w, 500, "internal")
	}
}

func writeEmailOTPRateError(w http.ResponseWriter, message string, retryAfter int) {
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": message, "retry_after_seconds": retryAfter})
}
