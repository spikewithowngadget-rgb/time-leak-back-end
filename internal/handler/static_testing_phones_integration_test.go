package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time-leak/internal/service"
)

func TestLocalhostServer_StaticTestingPhones_20Users_EndToEnd(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	contacts := service.StaticTestingPhoneContacts()
	if len(contacts) != 20 {
		t.Fatalf("expected 20 static testing contacts, got %d", len(contacts))
	}

	for _, contact := range contacts {
		contact := contact
		t.Run(fmt.Sprintf("%02d_%s", contact.Index, strings.TrimPrefix(contact.Phone, "+")), func(t *testing.T) {
			password := fmt.Sprintf("StrongPass123!%02d", contact.Index)

			otpReqResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
				"phone": contact.Phone,
			})
			if otpReqResp.StatusCode != http.StatusOK {
				t.Fatalf("otp request status: got %d want 200", otpReqResp.StatusCode)
			}

			var otpReqBody struct {
				RequestID string `json:"request_id"`
			}
			decodeJSON(t, otpReqResp, &otpReqBody)
			if otpReqBody.RequestID == "" {
				t.Fatal("expected request_id")
			}

			verifyResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{
				"request_id": otpReqBody.RequestID,
				"code":       contact.Code,
			})
			if verifyResp.StatusCode != http.StatusOK {
				t.Fatalf("otp verify status: got %d want 200", verifyResp.StatusCode)
			}

			var verifyBody struct {
				VerificationToken string `json:"verification_token"`
				Phone             string `json:"phone"`
			}
			decodeJSON(t, verifyResp, &verifyBody)
			if verifyBody.VerificationToken == "" {
				t.Fatal("expected verification token")
			}
			if verifyBody.Phone != contact.Phone {
				t.Fatalf("verify phone: got %q want %q", verifyBody.Phone, contact.Phone)
			}

			registerResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/register", map[string]any{
				"phone":              contact.Phone,
				"password":           password,
				"confirm_password":   password,
				"verification_token": verifyBody.VerificationToken,
			})
			if registerResp.StatusCode != http.StatusCreated {
				t.Fatalf("register status: got %d want 201", registerResp.StatusCode)
			}

			var registerBody struct {
				User struct {
					UserID string `json:"userId"`
					Phone  string `json:"phone"`
				} `json:"user"`
			}
			decodeJSON(t, registerResp, &registerBody)
			if registerBody.User.UserID == "" {
				t.Fatal("expected user id")
			}
			if registerBody.User.Phone != contact.Phone {
				t.Fatalf("register phone: got %q want %q", registerBody.User.Phone, contact.Phone)
			}

			loginResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/login", map[string]any{
				"phone":    contact.Phone,
				"password": password,
			})
			if loginResp.StatusCode != http.StatusOK {
				t.Fatalf("login status: got %d want 200", loginResp.StatusCode)
			}

			var loginBody struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				User         struct {
					UserID string `json:"userId"`
					Phone  string `json:"phone"`
				} `json:"user"`
			}
			decodeJSON(t, loginResp, &loginBody)
			if loginBody.AccessToken == "" || loginBody.RefreshToken == "" {
				t.Fatal("expected access and refresh tokens")
			}
			if loginBody.User.UserID != registerBody.User.UserID {
				t.Fatalf("login user id: got %q want %q", loginBody.User.UserID, registerBody.User.UserID)
			}
			if loginBody.User.Phone != contact.Phone {
				t.Fatalf("login phone: got %q want %q", loginBody.User.Phone, contact.Phone)
			}

			meResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/me", nil, loginBody.AccessToken)
			if meResp.StatusCode != http.StatusOK {
				t.Fatalf("auth/me status: got %d want 200", meResp.StatusCode)
			}

			var meBody struct {
				UserUUID string `json:"user_uuid"`
				Phone    string `json:"phone"`
			}
			decodeJSON(t, meResp, &meBody)
			if meBody.UserUUID != registerBody.User.UserID {
				t.Fatalf("auth/me user_uuid: got %q want %q", meBody.UserUUID, registerBody.User.UserID)
			}
			if meBody.Phone != contact.Phone {
				t.Fatalf("auth/me phone: got %q want %q", meBody.Phone, contact.Phone)
			}
		})
	}
}
