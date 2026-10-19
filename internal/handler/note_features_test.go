package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"time-leak/internal/domain"
)

func createFeatureNote(t *testing.T, f *notesFixture, fields map[string]any) domain.Note {
	t.Helper()
	resp := doReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", fields, f.tokens[0])
	requireNoteStatus(t, resp, 201)
	var note domain.Note
	decodeJSON(t, resp, &note)
	return note
}

func TestNoteFeaturesTitlesLifecycleAndCompatibility(t *testing.T) {
	f := newNotesFixture(t)
	note := createFeatureNote(t, f, map[string]any{"note_type": "birthday", "note_date": "2036-02-29", "title": " <b>Мама 🎂</b> ", "lifecycle": "recurring", "recurrence_rule": "FREQ=YEARLY"})
	if note.Title != "Мама 🎂" || note.NoteType != "birthday" || note.Lifecycle != "recurring" || note.RecurrenceRule == nil || note.IsCompleted || note.TitleAudioURL != nil {
		t.Fatalf("bad note: %+v", note)
	}
	endpoint := "/api/v1/auth/notes/" + note.ID
	resp := doReqAuth(t, f.srv.URL, http.MethodPut, endpoint, map[string]any{"note_type": "unchanged-title"}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var updated domain.Note
	decodeJSON(t, resp, &updated)
	if updated.Title != note.Title || updated.Lifecycle != "recurring" {
		t.Fatal("omitted fields were reset")
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPut, endpoint, map[string]any{"title": nil}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	decodeJSON(t, resp, &updated)
	if updated.Title != "unchanged-title · 2036-02-29" {
		t.Fatalf("default title: %q", updated.Title)
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes?from=2037-02-28&to=2037-02-28&fields=meta", nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var page struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	decodeJSON(t, resp, &page)
	if page.Total != 1 || len(page.Data) != 1 || page.Data[0]["title"] != updated.Title || page.Data[0]["occurrences"].([]any)[0] != "2037-02-28" {
		t.Fatalf("bad recurrence metadata: %+v", page)
	}
	if _, ok := page.Data[0]["title_audio_url"]; ok {
		t.Fatal("metadata leaked file URL")
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes/by-date?date=2037-02-28", nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var day struct {
		Data  []domain.Note `json:"data"`
		Total int           `json:"total"`
	}
	decodeJSON(t, resp, &day)
	if day.Total != 1 || day.Data[0].NoteDate != "2036-02-29" || len(day.Data[0].Occurrences) != 1 {
		t.Fatalf("bad day: %+v", day)
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPut, endpoint, map[string]any{"lifecycle": "one_time"}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	decodeJSON(t, resp, &updated)
	if updated.RecurrenceRule != nil || updated.IsCompleted {
		t.Fatal("conversion failed")
	}
	for _, body := range []map[string]any{
		{"title": strings.Repeat("я", 201)}, {"lifecycle": "recurring"}, {"lifecycle": "invalid"}, {"lifecycle": nil},
		{"lifecycle": "recurring", "recurrence_rule": "FREQ=DAILY"}, {"recurrence_rule": "FREQ=YEARLY"}, {"title_audio_url": "https://evil.test/file"},
	} {
		resp = doReqAuth(t, f.srv.URL, http.MethodPut, endpoint, body, f.tokens[0])
		requireNoteStatus(t, resp, 400)
		resp.Body.Close()
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPut, endpoint, map[string]any{"title": "stolen"}, f.tokens[1])
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNoteFeaturesRecurringPaginationAndCompletion(t *testing.T) {
	f := newNotesFixture(t)
	for i, rule := range []string{"FREQ=WEEKLY", "FREQ=MONTHLY", "FREQ=YEARLY"} {
		createFeatureNote(t, f, map[string]any{"note_type": fmt.Sprint(i), "note_date": "2024-01-31", "lifecycle": "recurring", "recurrence_rule": rule})
	}
	past := createFeatureNote(t, f, map[string]any{"note_type": "past", "note_date": "2024-02-27"})
	today := createFeatureNote(t, f, map[string]any{"note_type": "today", "note_date": "2024-02-28"})
	if count, err := f.repo.CompletePastNotes(context.Background(), "2024-02-28"); err != nil || count != 1 {
		t.Fatalf("completion: count=%d err=%v", count, err)
	}
	stored, err := f.repo.GetNoteByID(context.Background(), past.ID)
	if err != nil || !stored.IsCompleted {
		t.Fatalf("not completed: %+v %v", stored, err)
	}
	stored, err = f.repo.GetNoteByID(context.Background(), today.ID)
	if err != nil || stored.IsCompleted {
		t.Fatal("today completed too early")
	}
	for page := 1; page <= 2; page++ {
		resp := doReqAuth(t, f.srv.URL, http.MethodGet, fmt.Sprintf("/api/v1/auth/notes?from=2024-02-28&to=2024-02-28&page=%d&limit=1", page), nil, f.tokens[0])
		requireNoteStatus(t, resp, 200)
		var body struct {
			Data    []domain.Note `json:"data"`
			Total   int           `json:"total"`
			HasMore bool          `json:"has_more"`
		}
		decodeJSON(t, resp, &body)
		if body.Total != 2 || len(body.Data) != 1 || body.HasMore != (page == 1) {
			t.Fatalf("bad recurrence page: %+v", body)
		}
	}
	resp := doReqAuth(t, f.srv.URL, http.MethodPut, "/api/v1/auth/notes/"+past.ID, map[string]any{"lifecycle": "recurring", "recurrence_rule": "FREQ=WEEKLY"}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var converted domain.Note
	decodeJSON(t, resp, &converted)
	if converted.IsCompleted {
		t.Fatal("conversion did not reset completion")
	}
}

func TestNoteFeaturesSharingExpiryPermissionsAndSignedFiles(t *testing.T) {
	f := newNotesFixture(t)
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	resp := doMultipartReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]string{"note_type": "photo", "note_date": "2036-10-18", "title": "<b>Семья</b>"}, []multipartTestFile{{FieldName: "files", FileName: "family.png", ContentType: "image/png", Content: imageBytes.Bytes()}}, f.tokens[0])
	requireNoteStatus(t, resp, 201)
	var note domain.Note
	decodeJSON(t, resp, &note)
	path := "/api/v1/auth/notes/" + note.ID + "/share"
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {f.admin, 403}, {f.tokens[1], 404}} {
		resp = doReqAuth(t, f.srv.URL, http.MethodPost, path, map[string]any{}, tc.token)
		requireNoteStatus(t, resp, tc.status)
		resp.Body.Close()
	}
	for _, hours := range []int{-1, 0, 8761} {
		resp = doReqAuth(t, f.srv.URL, http.MethodPost, path, map[string]any{"expires_in": hours}, f.tokens[0])
		requireNoteStatus(t, resp, 400)
		resp.Body.Close()
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPost, path, map[string]any{}, f.tokens[0])
	requireNoteStatus(t, resp, 201)
	var share struct {
		URL     string    `json:"share_url"`
		Token   string    `json:"share_token"`
		Expires time.Time `json:"expires_at"`
	}
	decodeJSON(t, resp, &share)
	if len(share.Token) != 12 || !strings.HasSuffix(share.URL, "/share/"+share.Token) || time.Until(share.Expires) < 167*time.Hour {
		t.Fatalf("bad share: %+v", share)
	}
	resp = doReq(t, f.srv.URL, http.MethodGet, "/api/v1/notes/shared/"+share.Token, nil)
	requireNoteStatus(t, resp, 200)
	var shared sharedNoteResponse
	decodeJSON(t, resp, &shared)
	if shared.Title != "Семья" || shared.ID != note.ID || len(shared.NoteFiles) != 1 || shared.FileSizes[0] != int64(imageBytes.Len()) || strings.Contains(shared.SharedBy, f.users[0].Phone) {
		t.Fatalf("bad public note: %+v", shared)
	}
	resp = doReq(t, shared.NoteFiles[0], http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 200)
	file, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(file, imageBytes.Bytes()) {
		t.Fatal("signed file content changed")
	}
	signed, _ := url.Parse(shared.NoteFiles[0])
	q := signed.Query()
	q.Set("expires", "9999999999")
	signed.RawQuery = q.Encode()
	resp = doReq(t, signed.String(), http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 403)
	resp.Body.Close()
	resp = doReq(t, share.URL, http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 200)
	html, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(html), `property="og:title" content="Семья"`) || !strings.Contains(string(html), "timeleak://note/"+share.Token) || strings.Contains(string(html), "ZgotmplZ") {
		t.Fatalf("bad OG page: %s", html)
	}
	previewStart := strings.Index(string(html), `property="og:image" content="`) + len(`property="og:image" content="`)
	previewURL := strings.Split(string(html)[previewStart:], `"`)[0]
	previewURL = strings.ReplaceAll(previewURL, "&amp;", "&")
	resp = doReq(t, previewURL, http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 200)
	preview, err := png.Decode(resp.Body)
	resp.Body.Close()
	if err != nil || preview.Bounds().Dx() != 600 {
		t.Fatalf("bad thumbnail: %v", err)
	}
	if _, err := f.db.Exec(`UPDATE share_tokens SET expires_at='2000-01-01T00:00:00Z' WHERE token=?`, share.Token); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/s/" + share.Token, "/share/" + share.Token, "/api/v1/notes/shared/" + share.Token} {
		resp = doReq(t, f.srv.URL, http.MethodGet, endpoint, nil)
		requireNoteStatus(t, resp, 410)
		resp.Body.Close()
	}
	resp = doReq(t, shared.NoteFiles[0], http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 410)
	resp.Body.Close()
	resp = doReqAuth(t, f.srv.URL, http.MethodDelete, "/api/v1/auth/notes/"+note.ID, nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	resp.Body.Close()
	resp = doReq(t, f.srv.URL, http.MethodGet, "/api/v1/notes/shared/"+share.Token, nil)
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
	var remaining int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM share_tokens`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("share token not cascaded")
	}
}

func generatedTitleAudio(t *testing.T, extension string, seconds int) []byte {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for audio integration tests")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is required for audio integration tests")
	}
	file := filepath.Join(t.TempDir(), "title"+extension)
	cmd := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", fmt.Sprint(seconds), file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate audio: %v %s", err, output)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestNoteFeaturesTitleAudioValidationReplacementAndDeletion(t *testing.T) {
	f := newNotesFixture(t)
	created := doMultipartReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]string{"note_type": "voice", "title": "Текст", "note_date": "2036-10-18"},
		[]multipartTestFile{{FieldName: "title_audio", FileName: "title.m4a", ContentType: "audio/mp4", Content: generatedTitleAudio(t, ".m4a", 1)}}, f.tokens[0])
	requireNoteStatus(t, created, 201)
	var note domain.Note
	decodeJSON(t, created, &note)
	if note.TitleAudioURL == nil || note.Title != "Текст" {
		t.Fatal("POST did not save both titles")
	}
	endpoint := "/api/v1/auth/notes/" + note.ID
	for _, ext := range []string{".m4a", ".aac", ".mp3", ".ogg"} {
		data := generatedTitleAudio(t, ext, 1)
		resp := doMultipartReqAuth(t, f.srv.URL, http.MethodPut, endpoint, nil, []multipartTestFile{{FieldName: "title_audio", FileName: "title" + ext, ContentType: "audio/mp4", Content: data}}, f.tokens[0])
		requireNoteStatus(t, resp, 200)
		decodeJSON(t, resp, &note)
		if note.TitleAudioURL == nil || note.Title != "Текст" || len(note.NoteFiles) != 0 || note.FileCount != 0 || !strings.Contains(*note.TitleAudioURL, "note-title-audio/"+note.ID+".m4a") {
			t.Fatalf("bad audio title: %+v", note)
		}
	}
	resp30 := doMultipartReqAuth(t, f.srv.URL, http.MethodPut, endpoint, nil, []multipartTestFile{{FieldName: "title_audio", FileName: "max.m4a", ContentType: "audio/mp4", Content: generatedTitleAudio(t, ".m4a", 30)}}, f.tokens[0])
	requireNoteStatus(t, resp30, 200)
	resp30.Body.Close()
	canonical := filepath.Join(f.cfg.NoteFilesPath, "note-title-audio", note.ID+".m4a")
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {f.tokens[1], 404}, {f.tokens[0], 200}} {
		request, err := http.NewRequest(http.MethodGet, *note.TitleAudioURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tc.token != "" {
			request.Header.Set("Authorization", "Bearer "+tc.token)
		}
		response, err := f.srv.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		requireNoteStatus(t, response, tc.status)
		response.Body.Close()
	}
	shareResp := doReqAuth(t, f.srv.URL, http.MethodPost, endpoint+"/share", nil, f.tokens[0])
	requireNoteStatus(t, shareResp, 201)
	var share struct {
		Token string `json:"share_token"`
	}
	decodeJSON(t, shareResp, &share)
	sharedResp := doReq(t, f.srv.URL, http.MethodGet, "/api/v1/notes/shared/"+share.Token, nil)
	requireNoteStatus(t, sharedResp, 200)
	var shared sharedNoteResponse
	decodeJSON(t, sharedResp, &shared)
	if shared.TitleAudioURL == nil {
		t.Fatal("shared note lost audio title")
	}
	download := doReq(t, *shared.TitleAudioURL, http.MethodGet, "", nil)
	requireNoteStatus(t, download, 200)
	downloaded, _ := io.ReadAll(download.Body)
	download.Body.Close()
	if !bytes.Equal(downloaded, before) {
		t.Fatal("signed audio content changed")
	}
	invalid := []struct {
		name   string
		data   []byte
		status int
	}{
		{"fake.m4a", []byte("not an audio file"), 400},
		{"wrong.wav", []byte("not an audio file"), 400},
		{"huge.mp3", bytes.Repeat([]byte{1}, maxTitleAudioBytes+1), 413},
		{"long.m4a", generatedTitleAudio(t, ".m4a", 31), 400},
	}
	for _, tc := range invalid {
		resp := doMultipartReqAuth(t, f.srv.URL, http.MethodPut, endpoint, nil, []multipartTestFile{{FieldName: "title_audio", FileName: tc.name, ContentType: "audio/mp4", Content: tc.data}}, f.tokens[0])
		requireNoteStatus(t, resp, tc.status)
		resp.Body.Close()
		after, err := os.ReadFile(canonical)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("failed upload replaced the current audio")
		}
	}
	// A valid audio replacement followed by a rejected database mutation must roll back.
	resp := doMultipartReqAuth(t, f.srv.URL, http.MethodPut, endpoint, map[string]string{"note_date": "2037-02-29"}, []multipartTestFile{{FieldName: "title_audio", FileName: "valid.m4a", ContentType: "audio/mp4", Content: generatedTitleAudio(t, ".m4a", 1)}}, f.tokens[0])
	requireNoteStatus(t, resp, 400)
	resp.Body.Close()
	after, _ := os.ReadFile(canonical)
	if !bytes.Equal(before, after) {
		t.Fatal("failed note update destroyed previous audio")
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodDelete, endpoint+"/title-audio", nil, f.tokens[1])
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
	resp = doReqAuth(t, f.srv.URL, http.MethodDelete, endpoint+"/title-audio", nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	decodeJSON(t, resp, &note)
	if note.TitleAudioURL != nil || note.Title != "Текст" {
		t.Fatal("audio removal changed text title")
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Fatal("audio file not removed")
	}
	download = doReq(t, *shared.TitleAudioURL, http.MethodGet, "", nil)
	requireNoteStatus(t, download, 404)
	download.Body.Close()
	resp = doReqAuth(t, f.srv.URL, http.MethodDelete, endpoint+"/title-audio", nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	resp.Body.Close()
}

func TestNotePayloadRejectsTrailingJSON(t *testing.T) {
	f := newNotesFixture(t)
	note := createFeatureNote(t, f, map[string]any{"note_type": "calendar"})
	for _, endpoint := range []string{"/api/v1/auth/notes", "/api/v1/auth/notes/" + note.ID, "/api/v1/auth/notes/" + note.ID + "/share"} {
		method := http.MethodPost
		if endpoint == "/api/v1/auth/notes/"+note.ID {
			method = http.MethodPut
		}
		request, err := http.NewRequest(method, f.srv.URL+endpoint, strings.NewReader(`{} {"unexpected":true}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+f.tokens[0])
		request.Header.Set("Content-Type", "application/json")
		response, err := f.srv.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		requireNoteStatus(t, response, 400)
		response.Body.Close()
	}
}

func TestSharePreviewSupportsWebP(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for WebP integration")
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "image.webp")
	data, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f := newNotesFixture(t)
	h := &Handler{cfg: f.cfg}
	thumbnail := h.extraImageThumbnail(context.Background(), output)
	if thumbnail == nil || thumbnail.Bounds().Dx() != 315 || thumbnail.Bounds().Dy() != 315 {
		t.Fatal("WebP thumbnail missing or incorrectly sized")
	}
}
