package handler

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"time-leak/config"
)

func createLandingShare(t *testing.T, f *notesFixture, title string) string {
	t.Helper()
	note := createFeatureNote(t, f, map[string]any{"note_type": "calendar", "title": title, "note_date": "2036-10-19"})
	resp := doReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes/"+note.ID+"/share", nil, f.tokens[0])
	requireNoteStatus(t, resp, http.StatusCreated)
	var share struct {
		Token string `json:"share_token"`
	}
	decodeJSON(t, resp, &share)
	return share.Token
}

func readLandingMetadata(t *testing.T, f *notesFixture, token string) sharedNotePageData {
	t.Helper()
	resp := doReq(t, f.srv.URL, http.MethodGet, "/api/v1/notes/shared/"+token+"/landing", nil)
	requireNoteStatus(t, resp, http.StatusOK)
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected metadata headers: %v", resp.Header)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(body), "{}<>") {
		t.Fatal("metadata contains unencoded template or HTML delimiters")
	}
	payload, err := base64.StdEncoding.DecodeString(string(body))
	if err != nil {
		t.Fatalf("metadata is not base64: %v", err)
	}
	var data sharedNotePageData
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatalf("metadata is not encoded JSON: %v", err)
	}
	return data
}

func TestSharedNoteLandingEncodesUntrustedMetadata(t *testing.T) {
	f := newNotesFixture(t)
	f.cfg.Sharing = config.SharingConfig{
		PublicBaseURL:      "https://timeleak.kz",
		AppleAppStoreID:    "6762095094",
		AndroidPackageName: "kz.timeleak.app",
	}
	const title = `{{ env "PRIVATE_KEY" }} & "Заметка"`
	token := createLandingShare(t, f, title)
	data := readLandingMetadata(t, f, token)
	if data.Status != http.StatusOK || data.Title != title || data.Date != "2036-10-19" {
		t.Fatalf("unexpected note metadata: %+v", data)
	}
	if data.Description != "Заметка в TimeLeak • 2036-10-19" || data.URL != "https://timeleak.kz/share/"+token || data.DeepLink != "timeleak://share/"+token {
		t.Fatalf("unexpected sharing links: %+v", data)
	}
	if !strings.HasPrefix(data.Image, "https://timeleak.kz/api/v1/notes/shared/"+token+"/preview?") ||
		data.AppStoreURL != "https://apps.apple.com/app/id6762095094" ||
		data.PlayStoreURL != "https://play.google.com/store/apps/details?id=kz.timeleak.app" {
		t.Fatalf("unexpected preview or store links: %+v", data)
	}
	resp := doReq(t, f.srv.URL, http.MethodGet, "/share/"+token, nil)
	requireNoteStatus(t, resp, http.StatusOK)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`href="timeleak://share/` + token + `"`, `{{ env &#34;PRIVATE_KEY&#34; }} &amp; &#34;Заметка&#34;`, `property="og:title"`, `property="og:description"`, `property="og:image"`} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("fallback HTML missing %q: %s", expected, body)
		}
	}
	if strings.Contains(string(body), "ZgotmplZ") {
		t.Fatal("fallback template rejected the custom scheme")
	}
}

func TestSharedNoteLandingReturnsHumanErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		status int
		title  string
	}{
		{"invalid", "invalid", http.StatusNotFound, "Заметка не найдена"},
		{"missing", "unknown12345", http.StatusNotFound, "Заметка не найдена"},
		{"expired", "", http.StatusGone, "Срок действия ссылки истёк"},
		{"unavailable", "", http.StatusServiceUnavailable, "Не удалось загрузить заметку"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNotesFixture(t)
			token := tc.token
			if token == "" {
				token = createLandingShare(t, f, "Private note title")
				if tc.name == "expired" {
					if _, err := f.db.Exec(`UPDATE share_tokens SET expires_at='2000-01-01T00:00:00Z' WHERE token=?`, token); err != nil {
						t.Fatal(err)
					}
				} else if err := f.db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			data := readLandingMetadata(t, f, token)
			if data.Status != tc.status || data.Title != tc.title || data.Message == "" || data.Description == "" {
				t.Fatalf("unexpected error metadata: %+v", data)
			}
			if data.Image != "" || data.URL != "" || data.Date != "" || data.DeepLink != "" {
				t.Fatalf("error exposes note metadata: %+v", data)
			}
			for _, prefix := range []string{"/share/", "/s/"} {
				resp := doReq(t, f.srv.URL, http.MethodGet, prefix+token, nil)
				requireNoteStatus(t, resp, tc.status)
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.Header.Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(string(body), tc.title) || !strings.Contains(string(body), "<!doctype html>") {
					t.Fatalf("expected human HTML error: %s", body)
				}
				for _, forbidden := range []string{`{"error":`, "location.replace", "timeleak://", "Private note title"} {
					if strings.Contains(string(body), forbidden) {
						t.Errorf("error HTML contains %q", forbidden)
					}
				}
			}
		})
	}
}
