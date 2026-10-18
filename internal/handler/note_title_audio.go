package handler

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"time-leak/internal/domain"
)

const maxTitleAudioBytes = 2 << 20

var (
	errInvalidTitleAudio          = errors.New("title_audio must be a valid m4a, aac, mp3 or ogg audio recording of at most 30 seconds")
	errTitleAudioTooLarge         = errors.New("title_audio must not exceed 2 MiB")
	errAudioProcessingUnavailable = errors.New("audio processing unavailable")
)

func (h *Handler) lockNote(id string) func() {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	mu := &h.noteLocks[hash.Sum32()%uint32(len(h.noteLocks))]
	mu.Lock()
	return mu.Unlock
}

func (h *Handler) createNoteWithPayload(w http.ResponseWriter, r *http.Request, userID string, payload noteMutationPayload) {
	changes, err := domain.ResolveNoteMutation(domain.Note{}, payload.Changes)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	changes.ID = uuid.NewString()
	files, err := h.saveUploadedNoteFiles(payload.Files)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	audio, finish, err := h.saveTitleAudio(r.Context(), payload.TitleAudio, changes.ID)
	if err != nil {
		h.deleteStoredNoteFiles(files)
		h.writeNoteRequestError(w, err)
		return
	}
	committed := false
	defer func() { finish(committed) }()
	if payload.TitleAudio != nil {
		changes.TitleAudioURL = domain.StringChange(audio)
	}
	note, err := h.app.CreateNote(r.Context(), userID, payload.NoteType, files, payload.NoteDate, changes)
	if err != nil {
		h.deleteStoredNoteFiles(files)
		h.writeNoteRequestError(w, err)
		return
	}
	committed = true
	writeJSON(w, http.StatusCreated, h.noteResponse(r, note))
}

func (h *Handler) audioBinary(configured, fallback string) (string, error) {
	if configured == "" {
		configured = fallback
	}
	binary, err := exec.LookPath(configured)
	if err != nil {
		return "", errAudioProcessingUnavailable
	}
	return binary, nil
}

func probeAudio(ctx context.Context, binary, file string) (float64, error) {
	output, err := exec.CommandContext(ctx, binary, "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration,format_name:stream=codec_type,duration", "-of", "json", file).Output()
	if err != nil {
		return 0, errInvalidTitleAudio
	}
	var data struct {
		Format struct {
			Duration string `json:"duration"`
			Name     string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Type     string `json:"codec_type"`
			Duration string `json:"duration"`
		} `json:"streams"`
	}
	if json.Unmarshal(output, &data) != nil {
		return 0, errInvalidTitleAudio
	}
	validFormat := false
	for _, name := range strings.Split(data.Format.Name, ",") {
		if name == "mp3" || name == "aac" || name == "ogg" || name == "m4a" || name == "mov" || name == "mp4" {
			validFormat = true
		}
	}
	if !validFormat {
		return 0, errInvalidTitleAudio
	}
	duration, _ := strconv.ParseFloat(data.Format.Duration, 64)
	hasAudio := false
	for _, stream := range data.Streams {
		if stream.Type != "audio" {
			continue
		}
		hasAudio = true
		value, _ := strconv.ParseFloat(stream.Duration, 64)
		if value > duration {
			duration = value
		}
	}
	if !hasAudio || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 30 {
		return 0, errInvalidTitleAudio
	}
	return duration, nil
}

// Transcode to a canonical M4A, then atomically replace the per-note file.
// Keep the previous file until the database mutation succeeds, so a failed PUT
// cannot destroy a working audio title.
func (h *Handler) saveTitleAudio(ctx context.Context, header *multipart.FileHeader, noteID string) (string, func(bool), error) {
	noop := func(bool) {}
	if header == nil {
		return "", noop, nil
	}
	if header.Size > maxTitleAudioBytes {
		return "", noop, errTitleAudioTooLarge
	}
	switch strings.ToLower(filepath.Ext(header.Filename)) {
	case ".m4a", ".aac", ".mp3", ".ogg":
	default:
		return "", noop, errInvalidTitleAudio
	}
	ffprobe, err := h.audioBinary(h.cfg.Sharing.FFprobePath, "ffprobe")
	if err != nil {
		return "", noop, err
	}
	ffmpeg, err := h.audioBinary(h.cfg.Sharing.FFmpegPath, "ffmpeg")
	if err != nil {
		return "", noop, err
	}
	dir, err := os.MkdirTemp("", "timeleak-title-audio-")
	if err != nil {
		return "", noop, err
	}
	defer os.RemoveAll(dir)
	src, err := header.Open()
	if err != nil {
		return "", noop, err
	}
	defer src.Close()
	input := filepath.Join(dir, "input"+strings.ToLower(filepath.Ext(header.Filename)))
	file, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", noop, err
	}
	n, copyErr := io.Copy(file, io.LimitReader(src, maxTitleAudioBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", noop, copyErr
	}
	if closeErr != nil {
		return "", noop, closeErr
	}
	if n > maxTitleAudioBytes {
		return "", noop, errTitleAudioTooLarge
	}
	processCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := probeAudio(processCtx, ffprobe, input); err != nil {
		return "", noop, err
	}
	output := filepath.Join(dir, "title.m4a")
	command := exec.CommandContext(processCtx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe", "-i", input, "-map", "0:a:0", "-vn", "-map_metadata", "-1", "-t", "31", "-c:a", "aac", "-b:a", "96k", "-ar", "44100", "-ac", "1", "-movflags", "+faststart", output)
	if err := command.Run(); err != nil {
		return "", noop, errInvalidTitleAudio
	}
	if _, err := probeAudio(processCtx, ffprobe, output); err != nil {
		return "", noop, err
	}
	encoded, err := os.ReadFile(output)
	if err != nil {
		return "", noop, err
	}
	if len(encoded) > maxTitleAudioBytes {
		return "", noop, errTitleAudioTooLarge
	}
	relative := path.Join("note-title-audio", noteID+".m4a")
	absolute, valid := h.noteFileAbsolutePath(relative)
	if !valid {
		return "", noop, errInvalidTitleAudio
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return "", noop, err
	}
	staged := absolute + "." + uuid.NewString() + ".tmp"
	if err := os.WriteFile(staged, encoded, 0o600); err != nil {
		return "", noop, err
	}
	defer os.Remove(staged)
	previous := absolute + "." + uuid.NewString() + ".bak"
	hadPrevious := false
	if err := os.Link(absolute, previous); err == nil {
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", noop, err
	}
	if err := os.Rename(staged, absolute); err != nil {
		_ = os.Remove(previous)
		return "", noop, err
	}
	finish := func(committed bool) {
		var err error
		if committed {
			err = os.Remove(previous)
		} else if hadPrevious {
			err = os.Rename(previous, absolute)
		} else {
			err = os.Remove(absolute)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			h.log.Error("finalize title audio", zap.Error(err))
		}
	}
	return relative, finish, nil
}

func (h *Handler) AuthDeleteTitleAudio(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.userClaimsFromRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	unlock := h.lockNote(id)
	defer unlock()
	note, err := h.app.GetNote(r.Context(), id, claims.UserUUID)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	changes := domain.NoteMutation{TitleAudioURL: domain.OptionalString{Set: true}}
	updated, err := h.app.UpdateNote(r.Context(), id, claims.UserUUID, note.NoteType, note.NoteFiles, nil, changes)
	if err != nil {
		h.writeNoteRequestError(w, err)
		return
	}
	if note.TitleAudioURL != nil {
		h.deleteStoredNoteFiles([]string{*note.TitleAudioURL})
	}
	writeJSON(w, http.StatusOK, h.noteResponse(r, updated))
}
