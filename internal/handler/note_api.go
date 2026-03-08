package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"

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

func (h *Handler) UpdateUserNote(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	noteID := strings.TrimSpace(r.PathValue("noteId"))
	if userID == "" || noteID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	h.updateNote(w, r, noteID, userID)
}

func (h *Handler) DeleteUserNote(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	noteID := strings.TrimSpace(r.PathValue("noteId"))
	if userID == "" || noteID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "bad request")
		return
	}

	h.deleteNote(w, r, noteID, userID)
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

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		writeErrorJSON(w, http.StatusNotFound, "not found")
		return
	}

	http.ServeFile(w, r, absPath)
}

func (h *Handler) updateNote(w http.ResponseWriter, r *http.Request, noteID, userID string) {
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

	updated, err := h.app.UpdateNote(r.Context(), noteID, userID, noteType, noteFiles)
	if err != nil {
		if replaceFiles {
			h.deleteStoredNoteFiles(noteFiles)
		}
		h.writeNoteRequestError(w, err)
		return
	}

	if replaceFiles {
		h.deleteStoredNoteFiles(existing.NoteFiles)
	}

	writeJSON(w, http.StatusOK, h.noteResponse(r, updated))
}

func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request, noteID, userID string) {
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) writeNoteRequestError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, sql.ErrNoRows):
		writeErrorJSON(w, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrNoteTypeRequired),
		errors.Is(err, service.ErrTooManyNoteFiles),
		errors.Is(err, errTooManyMultipartNoteFiles),
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
