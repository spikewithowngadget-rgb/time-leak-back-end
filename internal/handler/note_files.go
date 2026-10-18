package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"time-leak/internal/domain"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	maxNoteFiles           = 5
	maxNoteMultipartMemory = 32 << 20
	noteFilesURLPrefix     = "/api/v1/note-files"
	noteFilesFormField     = "files"
)

var errTooManyMultipartNoteFiles = errors.New("too many note files")

type noteMutationPayload struct {
	Changes          domain.NoteMutation
	TitleAudio       *multipart.FileHeader
	NoteType         string
	NoteTypeProvided bool
	NoteDate         *string
	UserID           string
	Files            []*multipart.FileHeader
}

type updateNoteReq struct {
	domain.NoteMutation
	NoteType *string `json:"note_type"`
	NoteDate *string `json:"note_date"`
}

var errInvalidNotePayload = errors.New("invalid note payload")

func decodeNoteJSONBody(r *http.Request, out any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errors.Join(errInvalidNotePayload, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errInvalidNotePayload
	}
	return nil
}

func parseAuthCreateNotePayload(r *http.Request) (noteMutationPayload, error) {
	if isMultipartFormRequest(r) {
		return parseMultipartNotePayload(r, false)
	}

	var req authCreateNoteReq
	if err := decodeNoteJSONBody(r, &req); err != nil {
		return noteMutationPayload{}, err
	}

	return noteMutationPayload{
		Changes:          req.NoteMutation,
		NoteType:         strings.TrimSpace(req.NoteType),
		NoteTypeProvided: true,
		NoteDate:         req.NoteDate,
	}, nil
}

func parseLegacyCreateNotePayload(r *http.Request) (noteMutationPayload, error) {
	if isMultipartFormRequest(r) {
		return parseMultipartNotePayload(r, true)
	}

	var req createNoteReq
	if err := decodeNoteJSONBody(r, &req); err != nil {
		return noteMutationPayload{}, err
	}

	return noteMutationPayload{
		Changes:          req.NoteMutation,
		UserID:           strings.TrimSpace(req.UserID),
		NoteType:         strings.TrimSpace(req.NoteType),
		NoteTypeProvided: true,
		NoteDate:         req.NoteDate,
	}, nil
}

func parseNoteUpdatePayload(r *http.Request) (noteMutationPayload, error) {
	if isMultipartFormRequest(r) {
		return parseMultipartNotePayload(r, false)
	}

	var req updateNoteReq
	if err := decodeNoteJSONBody(r, &req); err != nil {
		return noteMutationPayload{}, err
	}

	payload := noteMutationPayload{NoteDate: req.NoteDate, Changes: req.NoteMutation}
	if req.NoteType != nil {
		payload.NoteTypeProvided = true
		payload.NoteType = strings.TrimSpace(*req.NoteType)
	}

	return payload, nil
}

func parseMultipartNotePayload(r *http.Request, requireUserID bool) (noteMutationPayload, error) {
	if err := r.ParseMultipartForm(maxNoteMultipartMemory); err != nil {
		cleanupMultipartForm(r)
		return noteMutationPayload{}, errors.Join(errInvalidNotePayload, err)
	}

	payload := noteMutationPayload{
		Files: r.MultipartForm.File[noteFilesFormField],
	}
	if files := r.MultipartForm.File["title_audio"]; len(files) > 1 {
		cleanupMultipartForm(r)
		return noteMutationPayload{}, errInvalidNotePayload
	} else if len(files) == 1 {
		payload.TitleAudio = files[0]
	}
	for key, field := range map[string]*domain.OptionalString{"title": &payload.Changes.Title, "lifecycle": &payload.Changes.Lifecycle, "recurrence_rule": &payload.Changes.RecurrenceRule} {
		if values, ok := r.MultipartForm.Value[key]; ok {
			if len(values) != 1 {
				cleanupMultipartForm(r)
				return noteMutationPayload{}, errInvalidNotePayload
			}
			*field = domain.StringChange(values[0])
			if key != "lifecycle" && values[0] == "" {
				field.Value = nil
			}
		}
	}
	if len(payload.Files) > maxNoteFiles {
		cleanupMultipartForm(r)
		return noteMutationPayload{}, errTooManyMultipartNoteFiles
	}

	if _, ok := r.MultipartForm.Value["note_type"]; ok {
		payload.NoteTypeProvided = true
		payload.NoteType = strings.TrimSpace(r.FormValue("note_type"))
	}
	if values, ok := r.MultipartForm.Value["note_date"]; ok {
		if len(values) != 1 {
			cleanupMultipartForm(r)
			return noteMutationPayload{}, errInvalidNotePayload
		}
		date := values[0]
		payload.NoteDate = &date
	}
	if requireUserID {
		payload.UserID = strings.TrimSpace(r.FormValue("userId"))
	}

	return payload, nil
}

func cleanupMultipartForm(r *http.Request) {
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
}

func isMultipartFormRequest(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/form-data")
}

func (h *Handler) saveUploadedNoteFiles(files []*multipart.FileHeader) ([]string, error) {
	if len(files) == 0 {
		return []string{}, nil
	}
	if len(files) > maxNoteFiles {
		return nil, errTooManyMultipartNoteFiles
	}

	saved := make([]string, 0, len(files))
	for _, header := range files {
		relPath, err := h.saveUploadedNoteFile(header)
		if err != nil {
			h.deleteStoredNoteFiles(saved)
			return nil, err
		}
		saved = append(saved, relPath)
	}

	return saved, nil
}

func (h *Handler) saveUploadedNoteFile(header *multipart.FileHeader) (string, error) {
	src, err := header.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	category := detectNoteFileCategory(header)
	ext := detectNoteFileExtension(header)
	if ext == "" {
		ext = ".bin"
	}

	dirPath := filepath.Join(h.cfg.NoteFilesPath, category)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return "", err
	}

	fileName := uuid.NewString() + ext
	absPath := filepath.Join(dirPath, fileName)

	dst, err := os.Create(absPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}

	return path.Join(category, fileName), nil
}

func detectNoteFileCategory(header *multipart.FileHeader) string {
	contentType := strings.ToLower(strings.TrimSpace(header.Header.Get("Content-Type")))
	switch {
	case strings.HasPrefix(contentType, "audio/"):
		return "audio"
	case strings.HasPrefix(contentType, "image/"):
		return "photo"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	}

	switch strings.ToLower(filepath.Ext(header.Filename)) {
	case ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac":
		return "audio"
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic":
		return "photo"
	case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v":
		return "video"
	default:
		return "document"
	}
}

func detectNoteFileExtension(header *multipart.FileHeader) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(header.Filename)))
	if ext != "" {
		return ext
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		return ""
	}

	exts, err := mime.ExtensionsByType(contentType)
	if err != nil || len(exts) == 0 {
		return ""
	}

	return strings.ToLower(exts[0])
}

func (h *Handler) deleteStoredNoteFiles(noteFiles []string) {
	for _, noteFile := range noteFiles {
		absPath, ok := h.noteFileAbsolutePath(noteFile)
		if !ok {
			h.log.Warn("skip deleting invalid note file path", zap.String("path", noteFile))
			continue
		}

		if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			h.log.Warn("delete note file failed", zap.String("path", absPath), zap.Error(err))
		}
	}
}

func (h *Handler) noteResponse(r *http.Request, note domain.Note) domain.Note {
	if note.TitleAudioURL != nil {
		value := h.noteFileURL(r, *note.TitleAudioURL)
		note.TitleAudioURL = &value
	}
	if len(note.NoteFiles) == 0 {
		note.NoteFiles = []string{}
		return note
	}

	baseURL := requestBaseURL(r)
	files := make([]string, 0, len(note.NoteFiles))
	for _, noteFile := range note.NoteFiles {
		cleanPath := path.Clean("/" + strings.TrimLeft(filepath.ToSlash(noteFile), "/"))
		urlPath := path.Join(noteFilesURLPrefix, strings.TrimPrefix(cleanPath, "/"))
		if baseURL == "" {
			files = append(files, urlPath)
			continue
		}
		files = append(files, baseURL+urlPath)
	}

	note.NoteFiles = files
	return note
}

func (h *Handler) noteFileURL(r *http.Request, file string) string {
	return requestBaseURL(r) + path.Join(noteFilesURLPrefix, strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(file)), "/"))
}

func (h *Handler) noteListResponse(r *http.Request, notes []domain.Note) []domain.Note {
	out := make([]domain.Note, 0, len(notes))
	for _, note := range notes {
		out = append(out, h.noteResponse(r, note))
	}
	return out
}

func (h *Handler) noteFileAbsolutePath(noteFile string) (string, bool) {
	noteFile = filepath.Clean(strings.TrimSpace(noteFile))
	if noteFile == "." || noteFile == "" || strings.HasPrefix(noteFile, "..") || filepath.IsAbs(noteFile) {
		return "", false
	}

	baseAbs, err := filepath.Abs(h.cfg.NoteFilesPath)
	if err != nil {
		return "", false
	}
	fileAbs, err := filepath.Abs(filepath.Join(baseAbs, noteFile))
	if err != nil {
		return "", false
	}

	if fileAbs != baseAbs && !strings.HasPrefix(fileAbs, baseAbs+string(os.PathSeparator)) {
		return "", false
	}
	return fileAbs, true
}

func requestBaseURL(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}

	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return (&url.URL{
		Scheme: scheme,
		Host:   host,
	}).String()
}
