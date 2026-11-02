package handler

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"go.uber.org/zap"
	"time-leak/internal/service"
)

// This envelope is consumed by the trusted static landing template in Caddy.
// Keep the exported JSON field names in sync with deploy/share/index.html.
type sharedNotePageData struct {
	Status       int
	Title        string
	Description  string
	Date         string
	Image        string
	URL          string
	AppStoreURL  string
	PlayStoreURL string
	DeepLink     string
	Message      string
}

var shareLandingToken = regexp.MustCompile(`^[A-Za-z0-9_-]{12}$`)

func (h *Handler) sharedNoteLandingData(r *http.Request) sharedNotePageData {
	data := sharedNotePageData{
		Status:       http.StatusNotFound,
		Title:        "Заметка не найдена",
		Description:  "Эта ссылка на заметку недействительна или больше недоступна.",
		Message:      "Проверьте ссылку или попросите отправителя поделиться заметкой ещё раз.",
		AppStoreURL:  h.cfg.Sharing.AppStoreURL(),
		PlayStoreURL: h.cfg.Sharing.PlayStoreURL(),
	}
	if !shareLandingToken.MatchString(r.PathValue("token")) {
		return data
	}
	note, share, err := h.app.GetSharedNote(r.Context(), r.PathValue("token"), true)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrShareExpired):
			data.Status = http.StatusGone
			data.Title = "Срок действия ссылки истёк"
			data.Description = "Эта заметка больше недоступна по ссылке."
			data.Message = "Попросите отправителя создать новую ссылку на заметку."
		case errors.Is(err, sql.ErrNoRows), errors.Is(err, service.ErrInvalidSharedToken):
		default:
			data.Status = http.StatusServiceUnavailable
			data.Title = "Не удалось загрузить заметку"
			data.Description = "Заметка временно недоступна."
			data.Message = "Попробуйте открыть ссылку немного позже."
			h.log.Warn("load shared note landing", zap.Error(err))
		}
		return data
	}
	data.Status = http.StatusOK
	data.Title = note.Title
	data.Description = "Заметка в TimeLeak • " + note.NoteDate
	data.Date = note.NoteDate
	data.Image = h.signedShareURL(r, share, "preview")
	data.URL = h.publicBaseURL(r) + "/share/" + share.Token
	data.DeepLink = "timeleak://share/" + share.Token
	data.Message = "Сохраните заметку в приложении после просмотра."
	return data
}

// SharedNoteLanding always returns a successful transport response so Caddy
// can render human error pages using the Status carried inside the envelope.
// httpInclude evaluates its response as a template: encoding the entire JSON
// payload prevents user-controlled titles from becoming template instructions.
func (h *Handler) SharedNoteLanding(w http.ResponseWriter, r *http.Request) {
	data, err := json.Marshal(h.sharedNoteLandingData(r))
	if err != nil {
		h.log.Error("encode shared note landing", zap.Error(err))
		http.Error(w, "landing temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(data)))
}
