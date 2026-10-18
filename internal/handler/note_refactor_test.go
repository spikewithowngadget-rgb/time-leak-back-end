package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"time-leak/config"
	"time-leak/internal/domain"
	"time-leak/internal/repository"
	"time-leak/internal/service"
	"time-leak/traits/database"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type notesFixture struct {
	srv    *httptest.Server
	db     *sql.DB
	repo   *repository.Repository
	cfg    *config.Config
	users  [2]domain.User
	tokens [2]string
	admin  string
}

func newNotesFixture(t *testing.T) *notesFixture {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{DBPath: tmp, DBName: "test.db", NoteFilesPath: filepath.Join(tmp, "files"), MaxOpenConns: 3, MaxIdleConns: 3,
		JWT: config.JWTConfig{AccessSecret: "test-user", AdminSecret: "test-admin", Issuer: "notes-test", AccessTTL: time.Minute, AdminAccessTTL: time.Minute, RefreshTTL: time.Hour}}
	db, err := database.InitDatabase(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := repository.New(db)
	app := service.NewAppService(repo, time.Minute, zap.NewNop())
	jwt := service.NewAuthService(cfg.JWT, repo, zap.NewNop())
	f := &notesFixture{db: db, repo: repo, cfg: cfg}
	for i := range f.users {
		user, err := repo.CreateUserWithPhone(context.Background(), fmt.Sprintf("owner%d@example.test", i), fmt.Sprintf("+7701555667%d", i), "unused-hash", "en")
		if err != nil {
			t.Fatal(err)
		}
		pair, err := jwt.IssueUserTokens(context.Background(), user, "otp_whatsapp")
		if err != nil {
			t.Fatal(err)
		}
		f.users[i], f.tokens[i] = user, pair.AccessToken
	}
	admin, err := jwt.IssueAdminToken(context.Background(), "test-admin")
	if err != nil {
		t.Fatal(err)
	}
	f.admin = admin.AccessToken
	mux := http.NewServeMux()
	New(&service.Services{App: app, JWT: jwt}, cfg, zap.NewNop()).Register(mux)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func requireNoteStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		resp.Body.Close()
		t.Fatalf("%s %s: status %d, want %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, want)
	}
}

func TestNotesAPI_LegacyPaginationAndMetadata(t *testing.T) {
	f := newNotesFixture(t)
	for i := 0; i < 105; i++ {
		date := "2036-10-15"
		if i == 103 {
			date = "2036-10-14"
		}
		if i == 104 {
			date = "2036-10-16"
		}
		if _, err := f.repo.CreateNote(context.Background(), f.users[0].UserID, "calendar", []string{"audio/test.m4a"}, &date); err != nil {
			t.Fatal(err)
		}
	}
	date := "2036-10-15"
	if _, err := f.repo.CreateNote(context.Background(), f.users[1].UserID, "other-owner", nil, &date); err != nil {
		t.Fatal(err)
	}
	type page struct {
		Data    []map[string]any `json:"data"`
		Total   int              `json:"total"`
		Page    int              `json:"page"`
		Limit   int              `json:"limit"`
		HasMore bool             `json:"has_more"`
	}
	read := func(path string) page {
		resp := doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, f.tokens[0])
		requireNoteStatus(t, resp, 200)
		var body page
		decodeJSON(t, resp, &body)
		return body
	}
	legacyResp := doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes", nil, f.tokens[0])
	requireNoteStatus(t, legacyResp, 200)
	var legacy map[string]any
	decodeJSON(t, legacyResp, &legacy)
	items := legacy["data"].([]any)
	if len(legacy) != 2 || len(items) != 105 || legacy["total"] != float64(105) {
		t.Fatalf("legacy shape/count changed: keys=%d count=%d", len(legacy), len(items))
	}
	for _, item := range items {
		note := item.(map[string]any)
		if len(note) != 11 || note["note_files"] == nil || note["createdAt"] == nil || note["title"] == nil || note["lifecycle"] != "one_time" {
			t.Fatalf("legacy note shape changed: %v", note)
		}
	}
	base := "/api/v1/auth/notes?from=2036-10-15&to=2036-10-15"
	first := read(base + "&fields=meta")
	if len(first.Data) != 100 || first.Total != 103 || first.Page != 1 || first.Limit != 100 || !first.HasMore {
		t.Fatalf("bad first page: size=%d total=%d page=%d limit=%d more=%v", len(first.Data), first.Total, first.Page, first.Limit, first.HasMore)
	}
	for _, note := range first.Data {
		if _, ok := note["note_files"]; ok {
			t.Fatal("metadata leaked file URLs")
		}
		if _, ok := note["userId"]; ok {
			t.Fatal("metadata leaked owner")
		}
		if note["note_date"] != "2036-10-15" || note["note_date_source"] != "selected" || note["file_count"] != float64(1) || note["file_type"] != "audio" {
			t.Fatalf("bad metadata: %v", note)
		}
	}
	second := read(base + "&fields=meta&page=2")
	if len(second.Data) != 3 || second.Total != 103 || second.HasMore {
		t.Fatalf("bad second page: %+v", second)
	}
	full := read(base + "&limit=500")
	combined := append(first.Data, second.Data...)
	if len(full.Data) != 103 {
		t.Fatal("full range truncated")
	}
	for i, note := range full.Data {
		if note["id"] != combined[i]["id"] {
			t.Fatal("unstable page ordering")
		}
		if note["userId"] != f.users[0].UserID || len(note["note_files"].([]any)) != 1 {
			t.Fatal("incomplete full note")
		}
	}
	beyond := read(base + "&page=3")
	if beyond.Data == nil || len(beyond.Data) != 0 || beyond.Total != 103 || beyond.HasMore {
		t.Fatalf("bad empty page: %+v", beyond)
	}
	empty := read("/api/v1/auth/notes?from=2000-01-01&to=2000-01-01&fields=meta")
	if empty.Data == nil || empty.Total != 0 || empty.HasMore {
		t.Fatal("empty range must return []")
	}
	if got := read("/api/v1/auth/notes?from=2036-10-14&to=2036-10-16&limit=500"); got.Total != 105 {
		t.Fatal("inclusive range lost boundaries or leaked owner")
	}
	day := read("/api/v1/auth/notes/by-date?date=2036-10-15")
	if len(day.Data) != 103 || day.Total != 103 {
		t.Fatal("by-date must return the whole selected day")
	}
	for _, note := range day.Data {
		if note["note_date"] != "2036-10-15" || note["note_files"] == nil {
			t.Fatal("bad by-date note")
		}
	}
}

func TestNotesAPI_MutationsKeepDatesFilesAndMetadataConsistent(t *testing.T) {
	f := newNotesFixture(t)
	image := multipartTestFile{FieldName: "files", FileName: "image.JPG", ContentType: "image/jpeg", Content: []byte("image-content")}
	audio := multipartTestFile{FieldName: "files", FileName: "voice.M4A", ContentType: "audio/mp4", Content: []byte("audio-content")}
	resp := doMultipartReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]string{"note_type": "future", "note_date": "2036-02-29"}, []multipartTestFile{image, audio}, f.tokens[0])
	requireNoteStatus(t, resp, 201)
	var created domain.Note
	decodeJSON(t, resp, &created)
	if created.NoteDate != "2036-02-29" || created.NoteDateSource != "selected" || created.FileCount != 2 || created.FileType != "image" || len(created.NoteFiles) != 2 {
		t.Fatalf("bad created note: %+v", created)
	}
	if !strings.Contains(created.NoteFiles[0], "/api/v1/note-files/photo/") {
		t.Fatal("existing photo URLs changed")
	}
	for _, file := range created.NoteFiles {
		resp := doReq(t, file, http.MethodGet, "", nil)
		requireNoteStatus(t, resp, 200)
		resp.Body.Close()
	}
	path := "/api/v1/auth/notes/" + created.ID
	resp = doReqAuth(t, f.srv.URL, http.MethodPut, path, map[string]any{"note_type": "renamed"}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var renamed domain.Note
	decodeJSON(t, resp, &renamed)
	if renamed.NoteType != "renamed" || renamed.NoteDate != created.NoteDate || renamed.NoteDateSource != "selected" || !reflect.DeepEqual(renamed.NoteFiles, created.NoteFiles) || renamed.FileCount != 2 || renamed.FileType != "image" {
		t.Fatal("rename lost date, files or metadata")
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPut, path, map[string]any{"note_date": "2037-01-01"}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var moved domain.Note
	decodeJSON(t, resp, &moved)
	if moved.NoteDate != "2037-01-01" || !moved.CreatedAt.Equal(created.CreatedAt) || !reflect.DeepEqual(moved.NoteFiles, created.NoteFiles) {
		t.Fatal("date update changed creation time or files")
	}
	for date, want := range map[string]int{"2036-02-29": 0, "2037-01-01": 1} {
		resp := doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes/by-date?date="+date, nil, f.tokens[0])
		requireNoteStatus(t, resp, 200)
		var body struct {
			Data  []domain.Note `json:"data"`
			Total int           `json:"total"`
		}
		decodeJSON(t, resp, &body)
		if body.Total != want || len(body.Data) != want || body.Data == nil {
			t.Fatal("note did not move between calendar days")
		}
	}
	resp = doMultipartReqAuth(t, f.srv.URL, http.MethodPut, path, nil, []multipartTestFile{audio}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var replaced domain.Note
	decodeJSON(t, resp, &replaced)
	if replaced.FileCount != 1 || replaced.FileType != "audio" || replaced.NoteDate != moved.NoteDate {
		t.Fatal("replacement metadata or date incorrect")
	}
	for _, file := range created.NoteFiles {
		resp := doReq(t, file, http.MethodGet, "", nil)
		requireNoteStatus(t, resp, 404)
		resp.Body.Close()
	}
	resp = doMultipartReqAuth(t, f.srv.URL, http.MethodPut, path, map[string]string{"note_date": "2037-02-29"}, []multipartTestFile{image}, f.tokens[0])
	requireNoteStatus(t, resp, 400)
	resp.Body.Close()
	var storedFiles int
	if err := filepath.WalkDir(f.cfg.NoteFilesPath, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			storedFiles++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if storedFiles != 1 {
		t.Fatalf("failed update left %d files, want original 1", storedFiles)
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var afterFailure domain.Note
	decodeJSON(t, resp, &afterFailure)
	if !reflect.DeepEqual(afterFailure, replaced) {
		t.Fatal("failed update changed note")
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodDelete, path, nil, f.tokens[1])
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
	resp = doReq(t, replaced.NoteFiles[0], http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 200)
	resp.Body.Close()
	resp = doReqAuth(t, f.srv.URL, http.MethodDelete, path, nil, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	resp.Body.Close()
	resp = doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, f.tokens[0])
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
	resp = doReq(t, replaced.NoteFiles[0], http.MethodGet, "", nil)
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNotesAPI_LegacyCreateAndDateBackfillByClient(t *testing.T) {
	f := newNotesFixture(t)
	resp := doReq(t, f.srv.URL, http.MethodPost, "/api/v1/notes", map[string]any{"userId": f.users[0].UserID, "note_type": "legacy"})
	requireNoteStatus(t, resp, 201)
	var note domain.Note
	decodeJSON(t, resp, &note)
	if note.NoteDate != note.CreatedAt.UTC().Format(time.DateOnly) || note.NoteDateSource != "created_at" || note.FileCount != 0 || note.FileType != "" || note.NoteFiles == nil {
		t.Fatalf("legacy defaults changed: %+v", note)
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPut, "/api/v1/auth/notes/"+note.ID, map[string]any{"note_date": "2038-12-31"}, f.tokens[0])
	requireNoteStatus(t, resp, 200)
	var updated domain.Note
	decodeJSON(t, resp, &updated)
	if updated.NoteDate != "2038-12-31" || updated.NoteDateSource != "selected" || !updated.CreatedAt.Equal(note.CreatedAt) {
		t.Fatal("client could not restore legacy calendar date")
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{"note_type": "old-json"}, f.tokens[0])
	requireNoteStatus(t, resp, 201)
	decodeJSON(t, resp, &note)
	if note.NoteDateSource != "created_at" {
		t.Fatal("old auth JSON create no longer supported")
	}
	resp = doMultipartReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/notes", map[string]string{"userId": f.users[0].UserID, "note_type": "legacy-form", "note_date": "2038-01-01"}, nil, "")
	requireNoteStatus(t, resp, 201)
	decodeJSON(t, resp, &note)
	if note.NoteDate != "2038-01-01" || note.NoteDateSource != "selected" {
		t.Fatal("legacy multipart date not accepted")
	}
}

func TestNotesAPI_ValidationAndOwnership(t *testing.T) {
	f := newNotesFixture(t)
	date := "2036-10-15"
	note, err := f.repo.CreateNote(context.Background(), f.users[0].UserID, "owned", nil, &date)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/auth/notes?from=2036-10-15&to=2036-10-15", "/api/v1/auth/notes/by-date?date=2036-10-15", "/api/v1/auth/notes/" + note.ID} {
		for _, token := range []string{"", "invalid-token"} {
			resp := doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, token)
			requireNoteStatus(t, resp, 401)
			resp.Body.Close()
		}
		resp := doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, f.admin)
		requireNoteStatus(t, resp, 403)
		resp.Body.Close()
	}
	for _, id := range []string{note.ID, uuid.NewString()} {
		resp := doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes/"+id, nil, f.tokens[1])
		requireNoteStatus(t, resp, 404)
		resp.Body.Close()
	}
	resp := doReqAuth(t, f.srv.URL, http.MethodPut, "/api/v1/auth/notes/"+note.ID, map[string]any{"note_date": "2039-01-01"}, f.tokens[1])
	requireNoteStatus(t, resp, 404)
	resp.Body.Close()
	for _, query := range []string{
		"from=2036-10-15", "to=2036-10-15", "from=&to=", "page=1", "fields=meta", "limit=10",
		"from=2036-10-16&to=2036-10-15", "from=2037-02-29&to=2037-03-01", "from=2036-1-01&to=2036-10-15",
		"from=2036-10-15&to=2036-10-15&page=0", "from=2036-10-15&to=2036-10-15&page=-1",
		"from=2036-10-15&to=2036-10-15&limit=501", "from=2036-10-15&to=2036-10-15&limit=0",
		"from=2036-10-15&to=2036-10-15&limit=oops", "from=2036-10-15&to=2036-10-15&fields=all",
		"from=2036-10-15&to=2036-10-15&page=9223372036854775807&limit=500",
		"from=2036-10-15&from=2036-10-16&to=2036-10-15",
		"from=2036-10-15&to=2036-10-15&limit=1&limit=2",
	} {
		t.Run(query, func(t *testing.T) {
			resp := doReqAuth(t, f.srv.URL, http.MethodGet, "/api/v1/auth/notes?"+query, nil, f.tokens[0])
			requireNoteStatus(t, resp, 400)
			resp.Body.Close()
		})
	}
	for _, path := range []string{"/api/v1/auth/notes/not-a-uuid", "/api/v1/auth/notes/by-date", "/api/v1/auth/notes/by-date?date=2037-02-29", "/api/v1/auth/notes/by-date?date=2036-10-15&date=2036-10-16"} {
		resp := doReqAuth(t, f.srv.URL, http.MethodGet, path, nil, f.tokens[0])
		requireNoteStatus(t, resp, 400)
		resp.Body.Close()
	}
	for _, date := range []string{"", "2037-02-29", "2036-10-15T00:00:00Z", "0000-01-01"} {
		resp := doReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{"note_type": "bad-date", "note_date": date}, f.tokens[0])
		requireNoteStatus(t, resp, 400)
		resp.Body.Close()
	}
	resp = doReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{"note_type": 123}, f.tokens[0])
	requireNoteStatus(t, resp, 400)
	resp.Body.Close()
}
