package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

func TestLocalhostServer_PhoneOTPAndNotesFlow(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	resp := doReq(t, srv.URL, http.MethodGet, "/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status: got %d want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	phone := "+77015556677"
	otpReqResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
		"phone": phone,
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
	if adminBody.AccessToken == "" {
		t.Fatal("expected admin access token")
	}

	otpCodeResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/admin/testing/otp/latest?phone=%2B77015556677", nil, adminBody.AccessToken)
	if otpCodeResp.StatusCode != http.StatusOK {
		t.Fatalf("latest otp status: got %d want 200", otpCodeResp.StatusCode)
	}
	codeRaw, err := io.ReadAll(otpCodeResp.Body)
	if err != nil {
		t.Fatalf("read otp code: %v", err)
	}
	_ = otpCodeResp.Body.Close()
	otpCode := strings.TrimSpace(string(codeRaw))
	if len(otpCode) != 4 {
		t.Fatalf("expected 4-digit otp code, got %q", otpCode)
	}

	verifyResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{
		"request_id": otpReqBody.RequestID,
		"code":       otpCode,
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
	if verifyBody.Phone != phone {
		t.Fatalf("expected phone %q got %q", phone, verifyBody.Phone)
	}

	registerResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"phone":              phone,
		"password":           "StrongPass123!",
		"confirm_password":   "StrongPass123!",
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
	if registerBody.User.Phone != phone {
		t.Fatalf("expected phone %q got %q", phone, registerBody.User.Phone)
	}

	loginResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone":    phone,
		"password": "StrongPass123!",
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
		t.Fatalf("expected user id %q got %q", registerBody.User.UserID, loginBody.User.UserID)
	}

	meClaims := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/me", nil, loginBody.AccessToken)
	if meClaims.StatusCode != http.StatusOK {
		t.Fatalf("auth/me status: got %d want 200", meClaims.StatusCode)
	}
	var meClaimsBody struct {
		UserUUID string `json:"user_uuid"`
		Phone    string `json:"phone"`
	}
	decodeJSON(t, meClaims, &meClaimsBody)
	if meClaimsBody.UserUUID != registerBody.User.UserID {
		t.Fatalf("expected user_uuid %q got %q", registerBody.User.UserID, meClaimsBody.UserUUID)
	}
	if meClaimsBody.Phone != phone {
		t.Fatalf("expected phone %q got %q", phone, meClaimsBody.Phone)
	}

	authNoteCreate := doReqAuth(t, srv.URL, http.MethodPost, "/api/v1/auth/notes", map[string]any{
		"note_type": "deadline",
	}, loginBody.AccessToken)
	if authNoteCreate.StatusCode != http.StatusCreated {
		t.Fatalf("auth note create status: got %d want 201", authNoteCreate.StatusCode)
	}
	_ = authNoteCreate.Body.Close()

	authNoteList := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/auth/notes", nil, loginBody.AccessToken)
	if authNoteList.StatusCode != http.StatusOK {
		t.Fatalf("auth note list status: got %d want 200", authNoteList.StatusCode)
	}
	var authList struct {
		Total int `json:"total"`
	}
	decodeJSON(t, authNoteList, &authList)
	if authList.Total < 1 {
		t.Fatalf("expected at least 1 note, got %d", authList.Total)
	}

	refreshResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/refresh", map[string]any{
		"refresh_token": loginBody.RefreshToken,
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

	meProfile := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/me", nil, refreshBody.AccessToken)
	if meProfile.StatusCode != http.StatusOK {
		t.Fatalf("/me status: got %d want 200", meProfile.StatusCode)
	}
	var meProfileBody struct {
		UserID       string `json:"userId"`
		UserLanguage string `json:"userLanguage"`
	}
	decodeJSON(t, meProfile, &meProfileBody)
	if meProfileBody.UserID != registerBody.User.UserID {
		t.Fatalf("expected profile user id %q got %q", registerBody.User.UserID, meProfileBody.UserID)
	}

	updateLang := doReq(t, srv.URL, http.MethodPut, "/api/v1/users/"+registerBody.User.UserID+"/language", map[string]any{
		"userLanguage": "kk",
	})
	if updateLang.StatusCode != http.StatusOK {
		t.Fatalf("update language status: got %d want 200", updateLang.StatusCode)
	}
	_ = updateLang.Body.Close()

	userByID := doReq(t, srv.URL, http.MethodGet, "/api/v1/users/"+registerBody.User.UserID, nil)
	if userByID.StatusCode != http.StatusOK {
		t.Fatalf("get user status: got %d want 200", userByID.StatusCode)
	}
	var userByIDBody struct {
		UserLanguage string `json:"userLanguage"`
	}
	decodeJSON(t, userByID, &userByIDBody)
	if userByIDBody.UserLanguage != "kk" {
		t.Fatalf("expected userLanguage=kk got %q", userByIDBody.UserLanguage)
	}

	legacyCreate := doReq(t, srv.URL, http.MethodPost, "/api/v1/notes", map[string]any{
		"userId":    registerBody.User.UserID,
		"note_type": "reminder",
	})
	if legacyCreate.StatusCode != http.StatusCreated {
		t.Fatalf("legacy note create status: got %d want 201", legacyCreate.StatusCode)
	}
	_ = legacyCreate.Body.Close()

	legacyList := doReq(t, srv.URL, http.MethodGet, "/api/v1/users/"+registerBody.User.UserID+"/notes", nil)
	if legacyList.StatusCode != http.StatusOK {
		t.Fatalf("legacy note list status: got %d want 200", legacyList.StatusCode)
	}
	var legacyListBody struct {
		Total int `json:"total"`
	}
	decodeJSON(t, legacyList, &legacyListBody)
	if legacyListBody.Total < 2 {
		t.Fatalf("expected at least 2 total notes, got %d", legacyListBody.Total)
	}
}

func TestLocalhostServer_RegisterVerificationTokenSingleUse(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	phone := "+77017778899"

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

	otpReqResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
		"phone": phone,
	})
	if otpReqResp.StatusCode != http.StatusOK {
		t.Fatalf("otp request status: got %d want 200", otpReqResp.StatusCode)
	}
	var otpReqBody struct {
		RequestID string `json:"request_id"`
	}
	decodeJSON(t, otpReqResp, &otpReqBody)

	otpCodeResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/admin/testing/otp/latest?phone="+url.QueryEscape(phone), nil, adminBody.AccessToken)
	if otpCodeResp.StatusCode != http.StatusOK {
		t.Fatalf("latest otp status: got %d want 200", otpCodeResp.StatusCode)
	}
	codeRaw, err := io.ReadAll(otpCodeResp.Body)
	if err != nil {
		t.Fatalf("read otp code: %v", err)
	}
	_ = otpCodeResp.Body.Close()

	verifyResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{
		"request_id": otpReqBody.RequestID,
		"code":       strings.TrimSpace(string(codeRaw)),
	})
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("otp verify status: got %d want 200", verifyResp.StatusCode)
	}
	var verifyBody struct {
		VerificationToken string `json:"verification_token"`
	}
	decodeJSON(t, verifyResp, &verifyBody)

	registerPayload := map[string]any{
		"phone":              phone,
		"password":           "StrongPass123!",
		"confirm_password":   "StrongPass123!",
		"verification_token": verifyBody.VerificationToken,
	}

	firstRegisterResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/register", registerPayload)
	if firstRegisterResp.StatusCode != http.StatusCreated {
		t.Fatalf("first register status: got %d want 201", firstRegisterResp.StatusCode)
	}
	_ = firstRegisterResp.Body.Close()

	secondRegisterResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/register", registerPayload)
	if secondRegisterResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("second register status: got %d want 400", secondRegisterResp.StatusCode)
	}
	_ = secondRegisterResp.Body.Close()
}

func TestLocalhostServer_PasswordResetFlow(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	phone := "+77018889900"
	initialPassword := "StartPass123!"
	newPassword := "NextPass123!"

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

	registerOTPReq := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
		"phone": phone,
	})
	if registerOTPReq.StatusCode != http.StatusOK {
		t.Fatalf("register otp request status: got %d want 200", registerOTPReq.StatusCode)
	}
	var registerOTPReqBody struct {
		RequestID string `json:"request_id"`
	}
	decodeJSON(t, registerOTPReq, &registerOTPReqBody)

	registerOTPCodeResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/admin/testing/otp/latest?phone="+url.QueryEscape(phone), nil, adminBody.AccessToken)
	if registerOTPCodeResp.StatusCode != http.StatusOK {
		t.Fatalf("register latest otp status: got %d want 200", registerOTPCodeResp.StatusCode)
	}
	registerCodeRaw, err := io.ReadAll(registerOTPCodeResp.Body)
	if err != nil {
		t.Fatalf("read register otp: %v", err)
	}
	_ = registerOTPCodeResp.Body.Close()

	registerVerifyResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/verify", map[string]any{
		"request_id": registerOTPReqBody.RequestID,
		"code":       strings.TrimSpace(string(registerCodeRaw)),
	})
	if registerVerifyResp.StatusCode != http.StatusOK {
		t.Fatalf("register otp verify status: got %d want 200", registerVerifyResp.StatusCode)
	}
	var registerVerifyBody struct {
		VerificationToken string `json:"verification_token"`
	}
	decodeJSON(t, registerVerifyResp, &registerVerifyBody)

	registerResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"phone":              phone,
		"password":           initialPassword,
		"confirm_password":   initialPassword,
		"verification_token": registerVerifyBody.VerificationToken,
	})
	if registerResp.StatusCode != http.StatusCreated {
		t.Fatalf("register status: got %d want 201", registerResp.StatusCode)
	}
	_ = registerResp.Body.Close()

	time.Sleep(1100 * time.Millisecond)

	resetOTPReq := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/password-reset/otp/request", map[string]any{
		"phone": phone,
	})
	if resetOTPReq.StatusCode != http.StatusOK {
		t.Fatalf("reset otp request status: got %d want 200", resetOTPReq.StatusCode)
	}
	var resetOTPReqBody struct {
		RequestID string `json:"request_id"`
	}
	decodeJSON(t, resetOTPReq, &resetOTPReqBody)

	resetOTPCodeResp := doReqAuth(t, srv.URL, http.MethodGet, "/api/v1/admin/testing/otp/latest?phone="+url.QueryEscape(phone), nil, adminBody.AccessToken)
	if resetOTPCodeResp.StatusCode != http.StatusOK {
		t.Fatalf("reset latest otp status: got %d want 200", resetOTPCodeResp.StatusCode)
	}
	resetCodeRaw, err := io.ReadAll(resetOTPCodeResp.Body)
	if err != nil {
		t.Fatalf("read reset otp: %v", err)
	}
	_ = resetOTPCodeResp.Body.Close()

	resetVerifyResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/password-reset/otp/verify", map[string]any{
		"request_id": resetOTPReqBody.RequestID,
		"code":       strings.TrimSpace(string(resetCodeRaw)),
	})
	if resetVerifyResp.StatusCode != http.StatusOK {
		t.Fatalf("reset otp verify status: got %d want 200", resetVerifyResp.StatusCode)
	}
	var resetVerifyBody struct {
		VerificationToken string `json:"verification_token"`
	}
	decodeJSON(t, resetVerifyResp, &resetVerifyBody)
	if resetVerifyBody.VerificationToken == "" {
		t.Fatal("expected reset verification token")
	}

	confirmResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/password-reset/confirm", map[string]any{
		"phone":              phone,
		"new_password":       newPassword,
		"confirm_password":   newPassword,
		"verification_token": resetVerifyBody.VerificationToken,
	})
	if confirmResp.StatusCode != http.StatusOK {
		t.Fatalf("reset confirm status: got %d want 200", confirmResp.StatusCode)
	}
	_ = confirmResp.Body.Close()

	oldLoginResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone":    phone,
		"password": initialPassword,
	})
	if oldLoginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password login status: got %d want 401", oldLoginResp.StatusCode)
	}
	_ = oldLoginResp.Body.Close()

	newLoginResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"phone":    phone,
		"password": newPassword,
	})
	if newLoginResp.StatusCode != http.StatusOK {
		t.Fatalf("new password login status: got %d want 200", newLoginResp.StatusCode)
	}
	_ = newLoginResp.Body.Close()
}

func TestLocalhostServer_CORSPreflight(t *testing.T) {
	srv := newLocalTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/api/v1/auth/otp/request", nil)
	if err != nil {
		t.Fatalf("new request error: %v", err)
	}
	req.Header.Set("Origin", "https://docs.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("http request error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status: got %d want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin header: got %q want '*'", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods header")
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected Access-Control-Allow-Headers header")
	}
}

func TestLocalhostServer_AdminLatestOTP_NoTokenRequired(t *testing.T) {
	srv := newLocalTestServerWithTestingEndpoints(t, true)
	defer srv.Close()

	otpReqResp := doReq(t, srv.URL, http.MethodPost, "/api/v1/auth/otp/request", map[string]any{
		"phone": "+77012223344",
	})
	if otpReqResp.StatusCode != http.StatusOK {
		t.Fatalf("otp request status: got %d want 200", otpReqResp.StatusCode)
	}
	_ = otpReqResp.Body.Close()

	withoutToken := doReq(t, srv.URL, http.MethodGet, "/api/v1/admin/testing/otp/latest?phone=%2B77012223344", nil)
	if withoutToken.StatusCode != http.StatusOK {
		t.Fatalf("without token status: got %d want 200", withoutToken.StatusCode)
	}
	_ = withoutToken.Body.Close()
}

func TestLocalhostServer_SwaggerSpec_PhoneOnlyAuth(t *testing.T) {
	srv := newLocalTestServer(t)
	defer srv.Close()

	swaggerResp := doReq(t, srv.URL, http.MethodGet, "/swagger.json", nil)
	if swaggerResp.StatusCode != http.StatusOK {
		t.Fatalf("swagger status: got %d want 200", swaggerResp.StatusCode)
	}
	var spec map[string]any
	decodeJSON(t, swaggerResp, &spec)

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("swagger spec missing paths")
	}
	if _, ok := paths["/api/v1/auth/otp/request"]; !ok {
		t.Fatal("swagger spec missing /api/v1/auth/otp/request")
	}
	if _, ok := paths["/api/v1/auth/login"]; !ok {
		t.Fatal("swagger spec missing /api/v1/auth/login")
	}
	if _, ok := paths["/api/v1/auth/register"]; !ok {
		t.Fatal("swagger spec missing /api/v1/auth/register")
	}
	if _, ok := paths["/api/v1/auth/password-reset/otp/request"]; !ok {
		t.Fatal("swagger spec missing /api/v1/auth/password-reset/otp/request")
	}
	if _, ok := paths["/api/v1/users/login"]; ok {
		t.Fatal("swagger must not expose /api/v1/users/login")
	}
	if _, ok := paths["/api/v1/users/register"]; ok {
		t.Fatal("swagger must not expose /api/v1/users/register")
	}

	otpLatestPath, ok := paths["/api/v1/admin/testing/otp/latest"].(map[string]any)
	if !ok {
		t.Fatal("swagger missing admin testing otp path")
	}
	otpLatestGet, ok := otpLatestPath["get"].(map[string]any)
	if !ok {
		t.Fatal("swagger missing GET for admin testing otp path")
	}
	if _, hasSecurity := otpLatestGet["security"]; hasSecurity {
		t.Fatal("expected no security requirement for /api/v1/admin/testing/otp/latest")
	}

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("swagger missing components")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("swagger missing components.schemas")
	}
	otpReqInput, ok := schemas["OTPRequestInput"].(map[string]any)
	if !ok {
		t.Fatal("swagger missing OTPRequestInput schema")
	}
	required, ok := otpReqInput["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "phone" {
		t.Fatal("OTPRequestInput.required must be [phone]")
	}

	servers, ok := spec["servers"].([]any)
	if !ok || len(servers) == 0 {
		t.Fatal("swagger spec missing servers")
	}
	firstServer, ok := servers[0].(map[string]any)
	if !ok {
		t.Fatal("swagger spec first server has invalid shape")
	}
	if serverURL, _ := firstServer["url"].(string); serverURL != "/" {
		t.Fatalf("expected swagger server url '/', got %q", serverURL)
	}
}

func newLocalTestServer(t *testing.T) *httptest.Server {
	return newLocalTestServerWithTestingEndpoints(t, false)
}

func newLocalTestServerWithTestingEndpoints(t *testing.T, enableTestingEndpoints bool) *httptest.Server {
	t.Helper()

	tmp := t.TempDir()
	cfg := &config.Config{
		Addr:            ":0",
		DBPath:          tmp,
		DBName:          "test.db",
		MaxOpenConns:    5,
		MaxIdleConns:    5,
		ConnMaxLifetime: 2 * time.Minute,
		JWT: config.JWTConfig{
			AccessSecret:   "test-access-secret",
			AdminSecret:    "test-admin-secret",
			AccessTTL:      60 * time.Second,
			AdminAccessTTL: 60 * time.Second,
			RefreshTTL:     24 * time.Hour,
			Issuer:         "test-suite",
		},
		OTP: config.OTPConfig{
			HMACSecret:      "test-otp-secret",
			RequestCooldown: 1 * time.Second,
			MaxAttempts:     5,
			LockDuration:    2 * time.Minute,
			ExpiresIn:       5 * time.Minute,
		},
		Admin: config.AdminConfig{
			Username: "Admin",
			Password: "QRT123",
		},
		EnableTestingEndpoints: enableTestingEndpoints,
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
	h := New(svcs, cfg, log)

	mux := http.NewServeMux()
	h.Register(mux)

	return httptest.NewServer(WithCORS(mux))
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

func doReqAuth(t *testing.T, baseURL, method, path string, body any, token string) *http.Response {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

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
