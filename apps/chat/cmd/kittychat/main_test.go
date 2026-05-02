package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
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

func TestNewServerMountsHostedWebLogin(t *testing.T) {
	cfg := testConfig()
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/login/google", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rr.Code, rr.Body.String())
	}
	location := rr.Header().Get("Location")
	if !strings.HasPrefix(location, "http://api.test/auth/web/google?") {
		t.Fatalf("Location = %q, want API web OAuth redirect", location)
	}
	if !strings.Contains(location, "redirect_uri=http%3A%2F%2Fchat.test%2Fauth%2Fcallback") {
		t.Fatalf("Location missing chat callback: %q", location)
	}
}

func TestNewServerUsesBFFSessionForHostedRoutes(t *testing.T) {
	cfg := testConfig()
	router, err := newRouter(cfg)
	if err != nil {
		t.Fatalf("newRouter() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app/api/routes", nil)
	req.AddCookie(&http.Cookie{Name: "kittychat_session", Value: "missing"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for missing server-side session", rr.Code)
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

func TestNewCredentialVerifierUsesJWKSForDeviceCredentials(t *testing.T) {
	key := newTestRSAKey(t)
	kid := "test-key-1"
	jwks := newTestJWKSet(t, kid, &key.PublicKey)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	defer srv.Close()

	cfg := config.Config{
		JWKSURL: srv.URL,
	}
	verifier, err := newCredentialVerifier(cfg)
	if err != nil {
		t.Fatalf("newCredentialVerifier() error = %v", err)
	}

	claims, err := verifier.VerifyDevice(context.Background(), signTestRS256DeviceJWT(t, key, kid, "user_1", "dev_1"))
	if err != nil {
		t.Fatalf("VerifyDevice() error = %v", err)
	}
	if claims.UserID != "user_1" || claims.DeviceID != "dev_1" {
		t.Fatalf("device identity = %+v", claims)
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

func TestNewServerAcceptsJWKSDeviceCredentialOnDaemonConnect(t *testing.T) {
	key := newTestRSAKey(t)
	kid := "test-key-1"
	jwks := newTestJWKSet(t, kid, &key.PublicKey)
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	defer jwksServer.Close()

	cfg := config.Config{
		JWKSURL: jwksServer.URL,
		Version: "test",
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
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + signTestRS256DeviceJWT(t, key, kid, "user_1", "dev_1")}},
	})
	if err != nil {
		t.Fatalf("dial daemon websocket with JWKS device credential: %v", err)
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
		PublicBaseURL:  "http://chat.test",
		APIAuthBaseURL: "http://api.test/auth",
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

func newTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func newTestJWKSet(t *testing.T, kid string, key *rsa.PublicKey) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": kid,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	})
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	return body
}

func signTestRS256DeviceJWT(t *testing.T, key *rsa.PrivateKey, kid, userID, deviceID string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":     identity.IssuerKittyAPI,
		"sub":     "device:" + deviceID,
		"aud":     []string{identity.AudienceKittyChat},
		"scope":   []string{"daemon:connect"},
		"v":       identity.CredentialVersion2,
		"user_id": userID,
		"iat":     now.Unix(),
		"exp":     now.Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign RS256 device jwt: %v", err)
	}
	return signed
}
