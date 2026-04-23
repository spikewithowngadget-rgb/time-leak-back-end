package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	defaultPublicAPIBaseURL = "https://api.timeleak.kz"
	publicAPIReviewPhone    = "+77471231213"
	publicAPIReviewOTPCode  = "1111"
	runLiveAPITestsEnv      = "TIME_LEAK_RUN_LIVE_API_TESTS"
	publicAPIBaseURLEnv     = "TIME_LEAK_PUBLIC_API_BASE_URL"
)

func TestPublicAPI_Health(t *testing.T) {
	baseURL := publicAPIBaseURL(t)

	resp := doLiveReq(t, baseURL, http.MethodGet, "/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status: got %d want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
	}
	decodeJSON(t, resp, &body)
	if body.Status != "ok" {
		t.Fatalf("health body status: got %q want %q", body.Status, "ok")
	}
}

func TestPublicAPI_AppStoreReviewOTPFlow(t *testing.T) {
	baseURL := publicAPIBaseURL(t)

	otpReqResp := doLiveReq(t, baseURL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
		"phone": publicAPIReviewPhone,
	})
	if otpReqResp.StatusCode != http.StatusOK {
		t.Fatalf("otp request status: got %d want 200", otpReqResp.StatusCode)
	}

	var otpReqBody struct {
		RequestID        string `json:"request_id"`
		ExpiresInSeconds int    `json:"expires_in_seconds"`
	}
	decodeJSON(t, otpReqResp, &otpReqBody)
	if otpReqBody.RequestID == "" {
		t.Fatal("expected request id")
	}
	if otpReqBody.ExpiresInSeconds <= 0 {
		t.Fatalf("expected positive expires_in_seconds, got %d", otpReqBody.ExpiresInSeconds)
	}

	verifyResp := doLiveReq(t, baseURL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{
		"request_id": otpReqBody.RequestID,
		"code":       publicAPIReviewOTPCode,
	})
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("otp verify status: got %d want 200", verifyResp.StatusCode)
	}

	var verifyBody struct {
		VerificationToken string `json:"verification_token"`
		Phone             string `json:"phone"`
		ExpiresInSeconds  int    `json:"expires_in_seconds"`
	}
	decodeJSON(t, verifyResp, &verifyBody)
	if verifyBody.VerificationToken == "" {
		t.Fatal("expected verification token")
	}
	if verifyBody.Phone != publicAPIReviewPhone {
		t.Fatalf("verify phone: got %q want %q", verifyBody.Phone, publicAPIReviewPhone)
	}
	if verifyBody.ExpiresInSeconds <= 0 {
		t.Fatalf("expected positive expires_in_seconds, got %d", verifyBody.ExpiresInSeconds)
	}
}

func publicAPIBaseURL(t *testing.T) string {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping live API smoke tests in short mode")
	}
	if strings.TrimSpace(os.Getenv(runLiveAPITestsEnv)) != "1" {
		t.Skip("set TIME_LEAK_RUN_LIVE_API_TESTS=1 to run live API smoke tests")
	}

	baseURL := strings.TrimSpace(os.Getenv(publicAPIBaseURLEnv))
	if baseURL == "" {
		baseURL = defaultPublicAPIBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func doLiveReq(t *testing.T, baseURL, method, path string, body any) *http.Response {
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

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request error: %v", err)
	}
	return resp
}
