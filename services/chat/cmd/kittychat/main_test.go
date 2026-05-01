package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/golang-jwt/jwt/v5"
	"github.com/kittypaw-app/kittychat/internal/config"
	"github.com/kittypaw-app/kittychat/internal/identity"
	"github.com/kittypaw-app/kittychat/internal/protocol"
)

func TestNewServerBuildsRunnableRouter(t *testing.T) {
	cfg := testConfig()
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestNewServerUsesSeededCredentialVerifier(t *testing.T) {
	router, err := newRouter(testConfig())
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	wrongReq := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	wrongReq.Header.Set("Authorization", "Bearer wrong")
	wrongRR := httptest.NewRecorder()
	router.ServeHTTP(wrongRR, wrongReq)
	if wrongRR.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401; body=%s", wrongRR.Code, wrongRR.Body.String())
	}

	validReq := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/v1/models", nil)
	validReq.Header.Set("Authorization", "Bearer api_secret")
	validRR := httptest.NewRecorder()
	router.ServeHTTP(validRR, validReq)
	if validRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("valid token status = %d, want 503 offline; body=%s", validRR.Code, validRR.Body.String())
	}
}

func TestNewServerUsesJWTVerifierWhenConfigured(t *testing.T) {
	cfg := testConfig()
	cfg.APIToken = ""
	cfg.JWTSecret = "test-jwt-secret-with-at-least-32-bytes"
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/nodes/dev_1/accounts/alice/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+signTestJWT(t, cfg.JWTSecret, "user_1"))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("valid JWT status = %d, want 503 offline; body=%s", rr.Code, rr.Body.String())
	}
}

func TestNewServerAllowsJWTOnlyConfiguration(t *testing.T) {
	cfg := config.Config{
		BindAddr:  ":0",
		JWTSecret: "test-jwt-secret-with-at-least-32-bytes",
		Version:   "test",
	}
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestNewCredentialVerifierUsesJWTForDeviceCredentials(t *testing.T) {
	cfg := config.Config{
		JWTSecret: "test-jwt-secret-with-at-least-32-bytes",
	}
	verifier, err := newCredentialVerifier(cfg)
	if err != nil {
		t.Fatalf("newCredentialVerifier() error = %v", err)
	}

	claims, err := verifier.VerifyDevice(context.Background(), signTestDeviceJWT(t, cfg.JWTSecret, "user_1", "dev_1", []string{"alice", "bob"}))
	if err != nil {
		t.Fatalf("VerifyDevice() error = %v", err)
	}
	if claims.UserID != "user_1" || claims.DeviceID != "dev_1" {
		t.Fatalf("device identity = %+v", claims)
	}
	if len(claims.LocalAccountIDs) != 2 || claims.LocalAccountIDs[0] != "alice" || claims.LocalAccountIDs[1] != "bob" {
		t.Fatalf("local accounts = %+v, want [alice bob]", claims.LocalAccountIDs)
	}
}

func TestNewServerAcceptsJWTDeviceCredentialOnDaemonConnect(t *testing.T) {
	cfg := config.Config{
		JWTSecret: "test-jwt-secret-with-at-least-32-bytes",
		Version:   "test",
	}
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + signTestDeviceJWT(t, cfg.JWTSecret, "user_1", "dev_1", []string{"alice"})}},
	})
	if err != nil {
		t.Fatalf("dial daemon websocket with JWT device credential: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "test done") }()

	if err := wsjson.Write(ctx, conn, protocol.Frame{
		Type:            protocol.FrameHello,
		DeviceID:        "dev_1",
		LocalAccounts:   []string{"alice"},
		DaemonVersion:   "test",
		ProtocolVersion: protocol.ProtocolVersion1,
		Capabilities:    []protocol.Operation{protocol.OperationOpenAIModels},
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
}

func TestNewServerRejectsMismatchedDeviceJWTOnDaemonConnect(t *testing.T) {
	cfg := config.Config{
		JWTSecret: "test-jwt-secret-with-at-least-32-bytes",
		Version:   "test",
	}
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv.URL), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + signTestDeviceJWTWithSubject(t, cfg.JWTSecret, "device:other", "user_1", "dev_1", []string{"alice"})}},
	})
	if err == nil {
		t.Fatal("dial succeeded with mismatched device JWT subject")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", responseStatus(resp))
	}
}

func TestNewServerRejectsInvalidCredentialSeed(t *testing.T) {
	cfg := testConfig()
	cfg.APIToken = ""

	if _, err := newRouter(cfg); err == nil {
		t.Fatal("newRouter() error = nil, want invalid identity seed error")
	}
}

func TestNewServerRejectsInvalidJWTSecret(t *testing.T) {
	cfg := testConfig()
	cfg.APIToken = ""
	cfg.JWTSecret = ""

	if _, err := newRouter(cfg); err == nil {
		t.Fatal("newRouter() error = nil, want missing auth credential error")
	}
}

func testConfig() config.Config {
	return config.Config{
		BindAddr:       ":0",
		APIToken:       "api_secret",
		DeviceToken:    "dev_secret",
		UserID:         "user_1",
		DeviceID:       "dev_1",
		LocalAccountID: "alice",
		Version:        "test",
	}
}

func signTestJWT(t *testing.T, secret, userID string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":   identity.IssuerKittyAPI,
		"sub":   userID,
		"aud":   []string{identity.AudienceKittyAPI, identity.AudienceKittyChat},
		"scope": []string{"chat:relay", "models:read"},
		"v":     1,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func signTestDeviceJWT(t *testing.T, secret, userID, deviceID string, accounts []string) string {
	t.Helper()
	return signTestDeviceJWTWithSubject(t, secret, "device:"+deviceID, userID, deviceID, accounts)
}

func signTestDeviceJWTWithSubject(t *testing.T, secret, subject, userID, deviceID string, accounts []string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss":            identity.IssuerKittyAPI,
		"sub":            subject,
		"aud":            []string{identity.AudienceKittyChat},
		"scope":          []string{"daemon:connect"},
		"v":              1,
		"user_id":        userID,
		"device_id":      deviceID,
		"local_accounts": accounts,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign device jwt: %v", err)
	}
	return signed
}

func wsURL(serverURL string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http") + "/daemon/connect"
}

func responseStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
