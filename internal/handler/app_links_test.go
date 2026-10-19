package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"time-leak/config"
)

const androidFingerprint = "D2:62:FF:B6:A9:02:AC:48:EA:00:1B:B7:2A:89:A0:E3:31:8B:39:07:E8:05:92:B7:BF:F3:5C:A8:D6:DD:A0:19"

func TestAppLinkAssociations(t *testing.T) {
	f := newNotesFixture(t)
	f.cfg.Sharing = config.SharingConfig{
		AppleAppID:                    "9YQXS629VS.kz.timeleak.project.app",
		AndroidPackageName:            "kz.timeleak.project.app",
		AndroidSHA256CertFingerprints: []string{androidFingerprint},
	}
	for _, tc := range []struct{ path, want string }{
		{"/.well-known/apple-app-site-association", `{"applinks":{"apps":[],"details":[{"appID":"9YQXS629VS.kz.timeleak.project.app","paths":["/share/*"]}]}}`},
		{"/.well-known/assetlinks.json", `[{"relation":["delegate_permission/common.handle_all_urls"],"target":{"namespace":"android_app","package_name":"kz.timeleak.project.app","sha256_cert_fingerprints":["` + androidFingerprint + `"]}}]`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp := doReq(t, f.srv.URL, http.MethodGet, tc.path, nil)
			requireNoteStatus(t, resp, http.StatusOK)
			if resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("Location") != "" || resp.Request.URL.Path != tc.path {
				t.Fatalf("association must be JSON without redirects: %v", resp.Header)
			}
			var got, want any
			decodeJSON(t, resp, &got)
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("association mismatch: got %v want %v", got, want)
			}
			resp = doReq(t, f.srv.URL, http.MethodHead, tc.path, nil)
			requireNoteStatus(t, resp, http.StatusOK)
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || len(body) != 0 {
				t.Fatal("HEAD must have an empty body")
			}
		})
	}
	f.cfg.Sharing.AndroidSHA256CertFingerprints = nil
	resp := doReq(t, f.srv.URL, http.MethodGet, "/.well-known/assetlinks.json", nil)
	requireNoteStatus(t, resp, http.StatusServiceUnavailable)
	resp.Body.Close()
}

func TestShareURLUsesPublicDomainAndFallbackPage(t *testing.T) {
	f := newNotesFixture(t)
	f.cfg.Sharing = config.SharingConfig{
		PublicBaseURL:      "https://timeleak.kz",
		AppleAppStoreID:    "6762095094",
		AndroidPackageName: "kz.timeleak.project.app",
	}
	note := createFeatureNote(t, f, map[string]any{"note_type": "calendar", "title": "Планы", "note_date": "2036-10-19"})
	resp := doReqAuth(t, f.srv.URL, http.MethodPost, "/api/v1/auth/notes/"+note.ID+"/share", nil, f.tokens[0])
	requireNoteStatus(t, resp, http.StatusCreated)
	var share struct {
		URL   string `json:"share_url"`
		Token string `json:"share_token"`
	}
	decodeJSON(t, resp, &share)
	if share.URL != "https://timeleak.kz/share/"+share.Token {
		t.Fatalf("unexpected share URL: %s", share.URL)
	}
	for _, prefix := range []string{"/share/", "/s/"} {
		resp = doReq(t, f.srv.URL, http.MethodGet, prefix+share.Token, nil)
		requireNoteStatus(t, resp, http.StatusOK)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`property="og:url" content="` + share.URL + `"`,
			`href="timeleak://note/` + share.Token + `"`,
			`href="https://apps.apple.com/app/id6762095094"`,
			`href="https://play.google.com/store/apps/details?id=kz.timeleak.project.app"`,
			`location.replace(storeURL)`,
		} {
			if !strings.Contains(string(body), want) {
				t.Errorf("missing %s on %s", want, prefix)
			}
		}
		if strings.Contains(string(body), "ZgotmplZ") {
			t.Fatal("template rejected a URL")
		}
	}
	resp = doReq(t, f.srv.URL, http.MethodGet, "/share/unknown12345", nil)
	requireNoteStatus(t, resp, http.StatusNotFound)
	resp.Body.Close()
}
