package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"time-leak/internal/domain"
	"time-leak/internal/service"
)

func (h *Handler) AuthUpdateNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	noteID := strings.TrimSpace(r.PathValue("id"))
	if noteID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	h.updateNote(w, r, noteID, claims.UserUUID)
}

func (h *Handler) AuthDeleteNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}

	noteID := strings.TrimSpace(r.PathValue("id"))
	if noteID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	h.deleteNote(w, r, noteID, claims.UserUUID)
}

func (h *Handler) NoteFile(w http.ResponseWriter, r *http.Request) {
	filePath := strings.TrimSpace(r.PathValue("path"))
	if filePath == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	absPath, ok := h.noteFileAbsolutePath(filePath)
	if !ok {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}
	cleanPath := filepath.ToSlash(filepath.Clean(filePath))
	if strings.HasPrefix(cleanPath, "note-title-audio/") {
		claims, ok := h.userClaimsFromRequest(w, r)
		if !ok {
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(cleanPath, "note-title-audio/"), ".m4a")
		note, err := h.app.GetNote(r.Context(), id, claims.UserUUID)
		if err != nil {
			h.writeNoteRequestError(w, err)
			return
		}
		if note.TitleAudioURL == nil || *note.TitleAudioURL != cleanPath {
			writeErrorJSON(w, http.StatusNotFound, "not found")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		writeErrorJSON(w, http.StatusNotFound, "not found")
		return
	}

	http.ServeFile(w, r, absPath)
}

func (h *Handler) updateNote(w http.ResponseWriter, r *http.Request, noteID, userID string) {
	unlock := h.lockNote(noteID)
	defer unlock()
	existing, err := h.app.GetNote(r.Context(), noteID, userID)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}

	payload, err := parseNoteUpdatePayload(r)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	defer cleanupMultipartForm(r)
	payload.Changes, err = domain.ResolveNoteMutation(existing, payload.Changes)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}

	noteType := existing.NoteType
	if payload.NoteTypeProvided {
		noteType = payload.NoteType
	}

	noteFiles := existing.NoteFiles
	replaceFiles := len(payload.Files) > 0
	if replaceFiles {
		noteFiles, err = h.saveUploadedNoteFiles(payload.Files)
		if err != nil {
			h.writeNoteRequestError(w, err)
			return
		}
	}

	audioPath, finishAudio, err := h.saveTitleAudio(r.Context(), payload.TitleAudio, noteID)
	if err != nil {
		if replaceFiles {
			h.deleteStoredNoteFiles(noteFiles)
		}
		h.writeNoteRequestError(w, err)
		return
	}
	committed := false
	defer func() { finishAudio(committed) }()
	if payload.TitleAudio != nil {
		payload.Changes.TitleAudioURL = domain.StringChange(audioPath)
	}
	updated, err := h.app.UpdateNote(r.Context(), noteID, userID, noteType, noteFiles, payload.NoteDate, payload.Changes)
	if err != nil {
		if replaceFiles {
			h.deleteStoredNoteFiles(noteFiles)
		}
		h.writeNoteRequestError(w, err)
		return
	}
	committed = true

	if replaceFiles {
		h.deleteStoredNoteFiles(existing.NoteFiles)
	}

	writeJSON(w, http.StatusOK, h.noteResponse(r, updated))
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request, noteID, userID string) {
	unlock := h.lockNote(noteID)
	defer unlock()
	note, err := h.app.GetNote(r.Context(), noteID, userID)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}

	if err := h.app.DeleteNote(r.Context(), noteID, userID); err != nil {
		h.writeNoteRequestError(w, err)
		return
	}

	h.deleteStoredNoteFiles(note.NoteFiles)
	if note.TitleAudioURL != nil {
		h.deleteStoredNoteFiles([]string{*note.TitleAudioURL})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) writeNoteRequestError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, sql.ErrNoRows):
		writeErrorJSON(w, http.StatusNotFound, "not found")
	case errors.Is(err, errAudioProcessingUnavailable):
		writeErrorJSON(w, http.StatusServiceUnavailable, "audio processing unavailable")
	case errors.Is(err, errTitleAudioTooLarge):
		writeErrorJSON(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, errInvalidTitleAudio), errors.Is(err, domain.ErrInvalidNoteFeatures),
		errors.Is(err, service.ErrInvalidNoteDate), errors.Is(err, service.ErrInvalidNoteFilter), errors.Is(err, service.ErrInvalidShareExpiry):
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrNoteTypeRequired),
		errors.Is(err, service.ErrTooManyNoteFiles),
		errors.Is(err, errTooManyMultipartNoteFiles),
		errors.Is(err, errInvalidNotePayload),
		isNoteBadRequestError(err):
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
	case strings.Contains(strings.ToLower(err.Error()), "foreign key"):
		writeErrorJSON(w, http.StatusNotFound, "user not found")
	default:
		writeErrorJSON(w, http.StatusInternalServerError, "internal")
	}
}

func isNoteBadRequestError(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "must be valid uuid") ||
		strings.Contains(msg, "is empty") ||
		strings.Contains(msg, "multipart")
}
