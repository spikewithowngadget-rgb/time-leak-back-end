package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"time-leak/config"
	"time-leak/internal/repository"
	"time-leak/internal/service"
	"time-leak/traits/database"

	"go.uber.org/zap"
)

func TestLocalhostServer_UserAndNotesFlow(t *testing.T) {
	srv := newLocalTestServer(t)
	defer srv.Close()

	// 1) health endpoint
	resp := doReq(t, srv.URL, http.MethodGet, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status: got %d want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// 2) register success
	regResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/users/register", map[string]any{
		"email":        "user@example.com",
		"password":     "secret-123",
		"userLanguage": "ru",
	})
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("register status: got %d want 201", regResp.StatusCode)
	}
	var user struct {
		UserID       string `json:"userId"`
		Email        string `json:"email"`
		UserLanguage string `json:"userLanguage"`
	}
	decodeJSON(t, regResp, &user)
	if user.UserID == "" {
		t.Fatal("expected userId")
	}

	// 3) duplicate register (negative)
	dupResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/users/register", map[string]any{
		"email":        "user@example.com",
		"password":     "secret-123",
		"userLanguage": "ru",
	})
	if dupResp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate register status: got %d want 409", dupResp.StatusCode)
	}
	_ = dupResp.Body.Close()

	// 4) login wrong password (negative)
	loginBad := doReq(t, srv.URL, http.MethodPost, "/api/v1/users/login", map[string]any{
		"email":    "user@example.com",
		"password": "wrong",
	})
	if loginBad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login status: got %d want 401", loginBad.StatusCode)
	}
	_ = loginBad.Body.Close()

	// 5) login success
	loginOK := doReq(t, srv.URL, http.MethodPost, "/api/v1/users/login", map[string]any{
		"email":    "user@example.com",
		"password": "secret-123",
	})
	if loginOK.StatusCode != http.StatusOK {
		t.Fatalf("good login status: got %d want 200", loginOK.StatusCode)
	}
	_ = loginOK.Body.Close()

	// 6) update language
	langResp := doReq(t, srv.URL, http.MethodPut, "/api/v1/users/"+user.UserID+"/language", map[string]any{
		"userLanguage": "kk",
	})
	if langResp.StatusCode != http.StatusOK {
		t.Fatalf("language update status: got %d want 200", langResp.StatusCode)
	}
	_ = langResp.Body.Close()

	// 7) get user and verify language
	getUser := doReq(t, srv.URL, http.MethodGet, "/api/v1/users/"+user.UserID, nil)
	if getUser.StatusCode != http.StatusOK {
		t.Fatalf("get user status: got %d want 200", getUser.StatusCode)
	}
	var userAfter struct {
		UserLanguage string `json:"userLanguage"`
	}
	decodeJSON(t, getUser, &userAfter)
	if userAfter.UserLanguage != "kk" {
		t.Fatalf("expected userLanguage=kk, got %q", userAfter.UserLanguage)
	}

	// 8) create note success
	noteResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/notes", map[string]any{
		"userId":    user.UserID,
		"note_type": "deadline",
	})
	if noteResp.StatusCode != http.StatusCreated {
		t.Fatalf("create note status: got %d want 201", noteResp.StatusCode)
	}
	_ = noteResp.Body.Close()

	// 9) create note for missing user (negative)
	noteMissingUser := doReq(t, srv.URL, http.MethodPost, "/api/v1/notes", map[string]any{
		"userId":    "11111111-1111-1111-1111-111111111111",
		"note_type": "deadline",
	})
	if noteMissingUser.StatusCode != http.StatusNotFound {
		t.Fatalf("missing-user note status: got %d want 404", noteMissingUser.StatusCode)
	}
	_ = noteMissingUser.Body.Close()

	// 10) list notes
	listResp := doReq(t, srv.URL, http.MethodGet, "/api/v1/users/"+user.UserID+"/notes", nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list notes status: got %d want 200", listResp.StatusCode)
	}
	var notesBody struct {
		Total int `json:"total"`
	}
	decodeJSON(t, listResp, &notesBody)
	if notesBody.Total < 1 {
		t.Fatalf("expected at least 1 note, got %d", notesBody.Total)
	}

	// extra: swagger spec available
	swaggerResp := doReq(t, srv.URL, http.MethodGet, "/swagger.json", nil)
	if swaggerResp.StatusCode != http.StatusOK {
		t.Fatalf("swagger status: got %d want 200", swaggerResp.StatusCode)
	}
	var spec map[string]any
	decodeJSON(t, swaggerResp, &spec)
	if spec["openapi"] == nil {
		t.Fatal("expected openapi field in swagger spec")
	}
}

func newLocalTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	tmp := t.TempDir()
	cfg := &config.Config{
		Addr:            ":0",
		DBPath:          tmp,
		DBName:          "test.db",
		MaxOpenConns:    5,
		MaxIdleConns:    5,
		ConnMaxLifetime: 2 * time.Minute,
	}

	log := zap.NewNop()
	db, err := database.InitDatabase(cfg, log)
	if err != nil {
		t.Fatalf("InitDatabase error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	repos := repository.NewRepositories(db)
	svcs := service.NewServices(context.Background(), cfg, repos, log)
	h := New(svcs.App, svcs.JWT)

	mux := http.NewServeMux()
	h.Register(mux)

	return httptest.NewServer(mux)
}

func doReq(t *testing.T, baseURL, method, path string, body any) *http.Response {
	t.Helper()

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("json marshal error: %v", err)
		}
	}

	req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request error: %v", err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode response error: %v", err)
	}
}

func TestLocalhostServer_BaseURLIsLocalhost(t *testing.T) {
	srv := newLocalTestServer(t)
	defer srv.Close()

	u := srv.URL
	if u == "" {
		t.Fatal("expected server URL")
	}

	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	host := parsed.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && !strings.HasPrefix(host, "[::1]") {
		t.Fatalf("expected localhost server, got host %q", host)
	}
}
