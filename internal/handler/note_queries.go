package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"time-leak/internal/domain"
	"time-leak/internal/service"
)

func parseNoteListQuery(r *http.Request) (domain.NoteListFilter, bool, error) {
	filter := domain.NoteListFilter{Page: 1, Limit: 100}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return filter, false, service.ErrInvalidNoteFilter
	}
	for _, key := range []string{"from", "to", "fields", "page", "limit"} {
		if len(query[key]) > 1 {
			return filter, false, service.ErrInvalidNoteFilter
		}
	}
	if !query.Has("from") && !query.Has("to") {
		if query.Has("fields") || query.Has("page") || query.Has("limit") {
			return filter, false, service.ErrInvalidNoteFilter
		}
		return filter, true, nil
	}
	filter.From, filter.To = query.Get("from"), query.Get("to")
	if query.Has("fields") {
		if query.Get("fields") != "meta" {
			return filter, false, fmt.Errorf("%w; fields must be meta or omitted", service.ErrInvalidNoteFilter)
		}
		filter.MetadataOnly = true
	}
	if query.Has("page") {
		filter.Page, err = strconv.Atoi(query.Get("page"))
		if err != nil {
			return filter, false, service.ErrInvalidNoteFilter
		}
	}
	if query.Has("limit") {
		filter.Limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil {
			return filter, false, service.ErrInvalidNoteFilter
		}
	}
	return filter, false, nil
}

func (h *Handler) listNotesInRange(w http.ResponseWriter, r *http.Request, userID string, filter domain.NoteListFilter) {
	notes, total, err := h.app.ListNotesInRange(r.Context(), userID, filter)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	var data any
	if filter.MetadataOnly {
		metadata := make([]domain.NoteMetadata, 0, len(notes))
		for _, note := range notes {
			metadata = append(metadata, note.Metadata())
		}
		data = metadata
	} else {
		data = h.noteListResponse(r, notes)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": data, "total": total, "page": filter.Page, "limit": filter.Limit,
		"has_more": (filter.Page-1)*filter.Limit+len(notes) < total,
	})
}

func (h *Handler) AuthNotesByDate(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["date"]) != 1 {
		writeErrorJSON(w, http.StatusBadRequest, "date must be a valid calendar date in YYYY-MM-DD format")
		return
	}
	if !domain.ValidNoteDate(query.Get("date")) {
		writeErrorJSON(w, http.StatusBadRequest, "date must be a valid calendar date in YYYY-MM-DD format")
		return
	}
	notes, err := h.app.ListNotesByDate(r.Context(), claims.UserUUID, query.Get("date"))
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": h.noteListResponse(r, notes), "total": len(notes)})
}

func (h *Handler) AuthGetNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	note, err := h.app.GetNote(r.Context(), r.PathValue("id"), claims.UserUUID)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.noteResponse(r, note))
}

// Keep the exact original list shape for clients that have not migrated yet.
type legacyNoteResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	NoteType  string    `json:"note_type"`
	NoteFiles []string  `json:"note_files"`
	CreatedAt time.Time `json:"createdAt"`
}

func (h *Handler) legacyNoteListResponse(r *http.Request, notes []domain.Note) []legacyNoteResponse {
	out := make([]legacyNoteResponse, 0, len(notes))
	for _, note := range notes {
		note = h.noteResponse(r, note)
		out = append(out, legacyNoteResponse{note.ID, note.UserID, note.NoteType, note.NoteFiles, note.CreatedAt})
	}
	return out
}
