package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLocalhostServer_JWTAuth_10Users_Positive(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	adminLogin := doReq(t, srv.URL, http.MethodPost, "/api/v1/admin/auth/login", map[string]any{
		"username": "Admin",
		"password": "QRT123",
	})
	if adminLogin.StatusCode != http.StatusOK {
		t.Fatalf("admin login status: got %d want 200", adminLogin.StatusCode)
	}
	var adminBody struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, adminLogin, &adminBody)

	for i := 1; i <= 10; i++ {
		i := i
		t.Run(fmt.Sprintf("user-%02d", i), func(t *testing.T) {
			phone := fmt.Sprintf("+7701000%04d", i)

			otpReq := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
				"phone": phone,
			})
			if otpReq.StatusCode != http.StatusOK {
				t.Fatalf("otp request status: got %d want 200", otpReq.StatusCode)
			}
			var otpReqBody struct {
				RequestID string `json:"request_id"`
			}
			decodeJSON(t, otpReq, &otpReqBody)

			latestOTP := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/admin/testing/otp/latest?phone="+url.QueryEscape(phone), nil, adminBody.AccessToken)
			if latestOTP.StatusCode != http.StatusOK {
				t.Fatalf("latest otp status: got %d want 200", latestOTP.StatusCode)
			}
			codeRaw, err := io.ReadAll(latestOTP.Body)
			if err != nil {
				t.Fatalf("read latest otp error: %v", err)
			}
			_ = latestOTP.Body.Close()
			code := strings.TrimSpace(string(codeRaw))
			if len(code) != 4 {
				t.Fatalf("expected 4-digit otp, got %q", code)
			}

			verify := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{
				"request_id": otpReqBody.RequestID,
				"code":       code,
			})
			if verify.StatusCode != http.StatusOK {
				t.Fatalf("otp verify status: got %d want 200", verify.StatusCode)
			}
			var verifyBody struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			decodeJSON(t, verify, &verifyBody)
			if verifyBody.AccessToken == "" || verifyBody.RefreshToken == "" {
				t.Fatal("expected access and refresh tokens")
			}

			meResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/me", nil, verifyBody.AccessToken)
			if meResp.StatusCode != http.StatusOK {
				t.Fatalf("auth me status: got %d want 200", meResp.StatusCode)
			}
			_ = meResp.Body.Close()

			note1Resp := doReqAuth(t, srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{
				"note_type": fmt.Sprintf("deadline-%d-1", i),
			}, verifyBody.AccessToken)
			if note1Resp.StatusCode != http.StatusCreated {
				t.Fatalf("create note1 status: got %d want 201", note1Resp.StatusCode)
			}
			_ = note1Resp.Body.Close()

			note2Resp := doReqAuth(t, srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{
				"note_type": fmt.Sprintf("deadline-%d-2", i),
			}, verifyBody.AccessToken)
			if note2Resp.StatusCode != http.StatusCreated {
				t.Fatalf("create note2 status: got %d want 201", note2Resp.StatusCode)
			}
			_ = note2Resp.Body.Close()

			listResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/notes", nil, verifyBody.AccessToken)
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

			refreshResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
				"refresh_token": verifyBody.RefreshToken,
			})
			if refreshResp.StatusCode != http.StatusOK {
				t.Fatalf("refresh status: got %d want 200", refreshResp.StatusCode)
			}
			var refreshBody struct {
				AccessToken string `json:"access_token"`
			}
			decodeJSON(t, refreshResp, &refreshBody)
			if refreshBody.AccessToken == "" {
				t.Fatal("expected refreshed access token")
			}

			me2Resp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/me", nil, refreshBody.AccessToken)
			if me2Resp.StatusCode != http.StatusOK {
				t.Fatalf("auth me after refresh status: got %d want 200", me2Resp.StatusCode)
			}
			_ = me2Resp.Body.Close()
		})
	}
}

func TestLocalhostServer_JWTAuth_NegativeSecurity(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		body       any
		token      string
		wantStatus int
	}{
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
		{
			name:       "admin latest otp without token",
			method:     http.MethodGet,
			path:       "/api/v1/admin/testing/otp/latest?phone=%2B77015556677",
			wantStatus: http.StatusNotFound,
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
