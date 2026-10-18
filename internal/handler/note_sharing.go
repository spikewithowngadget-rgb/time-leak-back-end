package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"time-leak/internal/domain"
	"time-leak/internal/service"
)

func (h *Handler) publicBaseURL(r *http.Request) string {
	if h.cfg.Sharing.PublicBaseURL != "" {
		return strings.TrimRight(h.cfg.Sharing.PublicBaseURL, "/")
	}
	return requestBaseURL(r)
}

func (h *Handler) AuthShareNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpiresIn *int `json:"expires_in"`
	}
	if r.ContentLength != 0 {
		if err := decodeNoteJSONBody(r, &req); err != nil {
			h.writeNoteRequestError(w, err)
			return
		}
	}
	hours := 168
	if req.ExpiresIn != nil {
		hours = *req.ExpiresIn
	}
	share, err := h.app.CreateNoteShare(r.Context(), r.PathValue("id"), claims.UserUUID, hours)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"share_url": h.publicBaseURL(r) + "/s/" + share.Token,
		"share_token": share.Token, "expires_at": share.ExpiresAt})
}

type sharedNoteResponse struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	NoteType       string    `json:"note_type"`
	NoteDate       string    `json:"note_date"`
	NoteFiles      []string  `json:"note_files"`
	FileSizes      []int64   `json:"file_sizes"`
	TitleAudioURL  *string   `json:"title_audio_url"`
	Lifecycle      string    `json:"lifecycle"`
	RecurrenceRule *string   `json:"recurrence_rule"`
	IsCompleted    bool      `json:"is_completed"`
	SharedBy       string    `json:"shared_by"`
	ExpiresAt      time.Time `json:"expires_at"`
}

func (h *Handler) SharedNote(w http.ResponseWriter, r *http.Request) {
	note, share, err := h.app.GetSharedNote(r.Context(), r.PathValue("token"), true)
	if err != nil {
		h.writeShareError(w, err)
		return
	}
	response := sharedNoteResponse{ID: note.ID, Title: note.Title, NoteType: note.NoteType, NoteDate: note.NoteDate,
		NoteFiles: []string{}, FileSizes: []int64{}, Lifecycle: note.Lifecycle, RecurrenceRule: note.RecurrenceRule,
		IsCompleted: note.IsCompleted, SharedBy: share.SharedBy, ExpiresAt: share.ExpiresAt}
	for i, file := range note.NoteFiles {
		response.NoteFiles = append(response.NoteFiles, h.signedShareURL(r, share, strconv.Itoa(i)))
		size := int64(0)
		if absolute, ok := h.noteFileAbsolutePath(file); ok {
			if info, err := os.Stat(absolute); err == nil {
				size = info.Size()
			}
		}
		response.FileSizes = append(response.FileSizes, size)
	}
	if note.TitleAudioURL != nil {
		value := h.signedShareURL(r, share, "title-audio")
		response.TitleAudioURL = &value
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) shareSignature(token, asset, expiry string) string {
	secret := h.cfg.Sharing.SigningSecret
	if secret == "" {
		secret = h.cfg.JWT.AccessSecret
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s\n%s\n%s", token, asset, expiry)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) signedShareURL(r *http.Request, share domain.NoteShare, asset string) string {
	expires := time.Now().UTC().Add(15 * time.Minute)
	if share.ExpiresAt.Before(expires) {
		expires = share.ExpiresAt
	}
	expiry := strconv.FormatInt(expires.Unix(), 10)
	suffix := "/files/" + asset
	if asset == "preview" {
		suffix = "/preview"
	}
	query := url.Values{"expires": {expiry}, "signature": {h.shareSignature(share.Token, asset, expiry)}}
	return h.publicBaseURL(r) + "/api/v1/notes/shared/" + share.Token + suffix + "?" + query.Encode()
}

func (h *Handler) validShareSignature(r *http.Request, asset string) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["expires"]) != 1 || len(query["signature"]) != 1 {
		return false
	}
	expires, err := strconv.ParseInt(query.Get("expires"), 10, 64)
	if err != nil || expires <= time.Now().UTC().Unix() {
		return false
	}
	got, err := hex.DecodeString(query.Get("signature"))
	if err != nil {
		return false
	}
	want, _ := hex.DecodeString(h.shareSignature(r.PathValue("token"), asset, query.Get("expires")))
	return hmac.Equal(got, want)
}

func (h *Handler) SharedNoteFile(w http.ResponseWriter, r *http.Request) {
	asset := r.PathValue("asset")
	if !h.validShareSignature(r, asset) {
		writeErrorJSON(w, http.StatusForbidden, "invalid or expired file URL")
		return
	}
	note, _, err := h.app.GetSharedNote(r.Context(), r.PathValue("token"), false)
	if err != nil {
		h.writeShareError(w, err)
		return
	}
	var file string
	if asset == "title-audio" && note.TitleAudioURL != nil {
		file = *note.TitleAudioURL
	} else {
		index, err := strconv.Atoi(asset)
		if err != nil || index < 0 || index >= len(note.NoteFiles) {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		file = note.NoteFiles[index]
	}
	absolute, ok := h.noteFileAbsolutePath(file)
	if !ok {
		writeErrorJSON(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, fileType := domain.NoteFileMetadata([]string{file})
	if asset != "title-audio" && fileType == "document" {
		w.Header().Set("Content-Disposition", "attachment")
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeFile(w, r, absolute)
}

func (h *Handler) writeShareError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrShareExpired):
		writeErrorJSON(w, http.StatusGone, "share link expired")
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, service.ErrInvalidSharedToken):
		writeErrorJSON(w, http.StatusNotFound, "not found")
	default:
		h.writeNoteRequestError(w, err)
	}
}

var sharedPage = template.Must(template.New("share").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}} — TimeLeak</title><meta property="og:type" content="article">
<meta property="og:title" content="{{.Title}}"><meta property="og:description" content="Заметка в TimeLeak • {{.Date}}">
<meta property="og:image" content="{{.Image}}"><meta property="og:image:width" content="600"><meta property="og:image:height" content="315">
<meta property="og:url" content="{{.URL}}">
<style>body{font:18px system-ui;background:#f3f5fa;color:#1d2740;margin:0;padding:48px 20px}main{max-width:560px;margin:auto;background:white;padding:32px;border-radius:24px}img{width:100%;border-radius:16px}a{display:inline-block;margin:12px 8px 0 0;padding:12px 18px;background:#334fcb;color:white;border-radius:12px;text-decoration:none}</style>
</head><body><main><p>TimeLeak</p><img src="{{.Image}}" alt="Превью заметки"><h1>{{.Title}}</h1><p>Заметка на {{.Date}}</p>
<a id="open-app" href="{{.DeepLink}}">Открыть в TimeLeak</a>
<p>Сохраните заметку в приложении после просмотра.</p></main>
<script>const deepLink={{.DeepLink}};if(/Android|iPhone|iPad|iPod/i.test(navigator.userAgent)){location.href=deepLink}</script>
</body></html>`))

func (h *Handler) SharedNotePage(w http.ResponseWriter, r *http.Request) {
	note, share, err := h.app.GetSharedNote(r.Context(), r.PathValue("token"), true)
	if err != nil {
		h.writeShareError(w, err)
		return
	}
	// The URI contains only a fixed scheme and a validated server-issued token.
	data := struct {
		Title, Date, Image, URL string
		DeepLink                template.URL
	}{
		note.Title, note.NoteDate, h.signedShareURL(r, share, "preview"), h.publicBaseURL(r) + "/s/" + share.Token,
		template.URL("timeleak://note/" + share.Token),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := sharedPage.Execute(w, data); err != nil {
		h.log.Warn("render shared note page")
	}
}
