package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestLocalhostServer_JWTAuth_10Users_Positive(t *testing.T) {
	srv := newLocalTestServer(t)
	defer srv.Close()

	for i := 1; i <= 10; i++ {
		i := i
		t.Run(fmt.Sprintf("user-%02d", i), func(t *testing.T) {
			email := fmt.Sprintf("user%02d@example.com", i)
			password := fmt.Sprintf("Pass#%02d", i)
			lang := "en"
			if i%2 == 0 {
				lang = "ru"
			}

			// 1) register
			regResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/users/register", map[string]any{
				"email":        email,
				"password":     password,
				"userLanguage": lang,
			})
			if regResp.StatusCode != http.StatusCreated {
				t.Fatalf("register status: got %d want 201", regResp.StatusCode)
			}
			var regBody struct {
				UserID string `json:"userId"`
				Email  string `json:"email"`
			}
			decodeJSON(t, regResp, &regBody)
			if regBody.UserID == "" {
				t.Fatal("expected userId")
			}

			// 2) JWT login
			loginResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/login", map[string]any{
				"email":    email,
				"password": password,
			})
			if loginResp.StatusCode != http.StatusOK {
				t.Fatalf("auth login status: got %d want 200", loginResp.StatusCode)
			}
			var loginBody struct {
				User struct {
					UserID string `json:"userId"`
					Email  string `json:"email"`
				} `json:"user"`
				Tokens struct {
					AccessToken  string `json:"access_token"`
					RefreshToken string `json:"refresh_token"`
				} `json:"tokens"`
			}
			decodeJSON(t, loginResp, &loginBody)
			if loginBody.Tokens.AccessToken == "" || loginBody.Tokens.RefreshToken == "" {
				t.Fatal("expected access and refresh tokens")
			}

			// 3) JWT me (protected)
			meResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/me", nil, loginBody.Tokens.AccessToken)
			if meResp.StatusCode != http.StatusOK {
				t.Fatalf("auth me status: got %d want 200", meResp.StatusCode)
			}
			var meBody struct {
				UserUUID string `json:"user_uuid"`
				Email    string `json:"email"`
			}
			decodeJSON(t, meResp, &meBody)
			if meBody.UserUUID != regBody.UserID {
				t.Fatalf("expected user_uuid %q, got %q", regBody.UserID, meBody.UserUUID)
			}

			// 4) create secure note #1
			note1Resp := doReqAuth(t, srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{
				"note_type": fmt.Sprintf("deadline-%d-1", i),
			}, loginBody.Tokens.AccessToken)
			if note1Resp.StatusCode != http.StatusCreated {
				t.Fatalf("create note1 status: got %d want 201", note1Resp.StatusCode)
			}
			_ = note1Resp.Body.Close()

			// 5) create secure note #2
			note2Resp := doReqAuth(t, srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{
				"note_type": fmt.Sprintf("deadline-%d-2", i),
			}, loginBody.Tokens.AccessToken)
			if note2Resp.StatusCode != http.StatusCreated {
				t.Fatalf("create note2 status: got %d want 201", note2Resp.StatusCode)
			}
			_ = note2Resp.Body.Close()

			// 6) list secure notes
			listResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/notes", nil, loginBody.Tokens.AccessToken)
			if listResp.StatusCode != http.StatusOK {
				t.Fatalf("list notes status: got %d want 200", listResp.StatusCode)
			}
			var listBody struct {
				Total int `json:"total"`
			}
			decodeJSON(t, listResp, &listBody)
			if listBody.Total < 2 {
				t.Fatalf("expected at least 2 notes, got %d", listBody.Total)
			}

			// 7) refresh
			refreshResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
				"refresh_token": loginBody.Tokens.RefreshToken,
			})
			if refreshResp.StatusCode != http.StatusOK {
				t.Fatalf("refresh status: got %d want 200", refreshResp.StatusCode)
			}
			var refreshBody struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			decodeJSON(t, refreshResp, &refreshBody)
			if refreshBody.AccessToken == "" || refreshBody.RefreshToken == "" {
				t.Fatal("expected refreshed token pair")
			}

			// 8) me with refreshed access
			me2Resp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/me", nil, refreshBody.AccessToken)
			if me2Resp.StatusCode != http.StatusOK {
				t.Fatalf("auth me after refresh status: got %d want 200", me2Resp.StatusCode)
			}
			_ = me2Resp.Body.Close()
		})
	}
}

func TestLocalhostServer_JWTAuth_NegativeSecurity(t *testing.T) {
	srv := newLocalTestServer(t)
	defer srv.Close()

	email := "negative-user@example.com"
	password := "Pass#99"

	regResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/users/register", map[string]any{
		"email":        email,
		"password":     password,
		"userLanguage": "en",
	})
	if regResp.StatusCode != http.StatusCreated {
		t.Fatalf("register status: got %d want 201", regResp.StatusCode)
	}
	_ = regResp.Body.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		body       any
		token      string
		wantStatus int
	}{
		{
			name:       "wrong password login",
			method:     http.MethodPost,
			path:       "/api/v1/auth/login",
			body:       map[string]any{"email": email, "password": "wrong-pass"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "me without token",
			method:     http.MethodGet,
			path:       "/api/v1/auth/me",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "me with invalid token",
			method:     http.MethodGet,
			path:       "/api/v1/auth/me",
			token:      "bad-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "refresh invalid token",
			method:     http.MethodPost,
			path:       "/api/v1/auth/refresh",
			body:       map[string]any{"refresh_token": "bad-refresh"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "secure create note without token",
			method:     http.MethodPost,
			path:       "/api/v1/auth/notes",
			body:       map[string]any{"note_type": "deadline"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "secure list notes without token",
			method:     http.MethodGet,
			path:       "/api/v1/auth/notes",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "secure create note invalid token",
			method:     http.MethodPost,
			path:       "/api/v1/auth/notes",
			body:       map[string]any{"note_type": "deadline"},
			token:      "bad-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "refresh empty token",
			method:     http.MethodPost,
			path:       "/api/v1/auth/refresh",
			body:       map[string]any{"refresh_token": " "},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp := doReqAuth(t, srv.URL, tt.method, tt.path, tt.body, tt.token)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status: got %d want %d", resp.StatusCode, tt.wantStatus)
			}
			_ = resp.Body.Close()
		})
	}
}

func doReqAuth(t *testing.T, baseURL, method, path string, body any, accessToken string) *http.Response {
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
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request error: %v", err)
	}
	return resp
}
