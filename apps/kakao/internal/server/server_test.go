package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kittypaw-app/kittykakao/internal/config"
	"github.com/kittypaw-app/kittykakao/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testConfig() config.Config {
	return config.Config{
		WebhookSecret: "test-secret",
		DailyLimit:    10_000,
		MonthlyLimit:  100_000,
		ChannelURL:    "https://pf.kakao.com/test",
		DatabasePath:  ":memory:",
		BindAddr:      "127.0.0.1:0",
	}
}

func spawnServer(t *testing.T) (*httptest.Server, chan []byte) {
	t.Helper()
	return spawnServerWithConfig(t, testConfig())
}

func spawnServerWithConfig(t *testing.T, cfg config.Config) (*httptest.Server, chan []byte) {
	t.Helper()
	s, err := store.Open(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	callbacks := make(chan []byte, 8)
	state := NewState(cfg, s, "test-version", "test-commit")
	state.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		callbacks <- raw
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    r,
		}, nil
	})}
	t.Cleanup(func() { _ = s.Close() })
	ts := httptest.NewServer(NewRouter(state))
	t.Cleanup(ts.Close)
	return ts, callbacks
}

func wsURL(ts *httptest.Server, token string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/" + token
}

func webhookPayload(actionID, utterance, userID string, callbackURL string) map[string]any {
	userRequest := map[string]any{
		"utterance": utterance,
		"user":      map[string]any{"id": userID},
	}
	if callbackURL != "" {
		userRequest["callbackUrl"] = callbackURL
	}
	return map[string]any{
		"action":      map[string]any{"id": actionID},
		"userRequest": userRequest,
	}
}

func decodeJSON(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return body
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func simpleText(body map[string]any) string {
	template := body["template"].(map[string]any)
	outputs := template["outputs"].([]any)
	output := outputs[0].(map[string]any)
	text := output["simpleText"].(map[string]any)
	return text["text"].(string)
}

func TestHappyPathRegisterPairWebhookWS(t *testing.T) {
	ts, callbacks := spawnServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	regResp := postJSON(t, ts.URL+"/register", nil)
	reg := decodeJSON(t, regResp)
	token := reg["token"].(string)
	pairCode := reg["pair_code"].(string)
	if len(token) != 32 {
		t.Fatalf("token length = %d, want 32", len(token))
	}
	if len(pairCode) != 6 {
		t.Fatalf("pair code length = %d, want 6", len(pairCode))
	}
	if reg["channel_url"].(string) == "" {
		t.Fatal("channel_url is empty")
	}

	conn, _, err := websocket.Dial(ctx, wsURL(ts, token), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	pairResp := postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("pair_act", pairCode, "kakao_user_1", "https://callback.kakao.com/pair"))
	pairBody := decodeJSON(t, pairResp)
	if got := simpleText(pairBody); got != "연결 완료!" {
		t.Fatalf("pair response = %q", got)
	}

	statusResp, err := http.Get(ts.URL + "/pair-status/" + token)
	if err != nil {
		t.Fatalf("get pair status: %v", err)
	}
	statusBody := decodeJSON(t, statusResp)
	if statusBody["paired"] != true {
		t.Fatalf("paired = %v", statusBody["paired"])
	}

	msgResp := postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_001", "안녕하세요", "kakao_user_1", "https://callback.kakao.com/reply"))
	msgBody := decodeJSON(t, msgResp)
	if msgBody["useCallback"] != true {
		t.Fatalf("useCallback = %v", msgBody["useCallback"])
	}

	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read websocket frame: %v", err)
	}
	if frame["id"] != "act_001" || frame["text"] != "안녕하세요" || frame["user_id"] != "kakao_user_1" {
		t.Fatalf("frame = %+v", frame)
	}

	if err := wsjson.Write(ctx, conn, map[string]string{"id": "act_001", "text": "반갑습니다"}); err != nil {
		t.Fatalf("write reply: %v", err)
	}
	select {
	case raw := <-callbacks:
		if !bytes.Contains(raw, []byte("반갑습니다")) {
			t.Fatalf("callback body = %s", raw)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for callback dispatch")
	}
}

func TestExpiredPairCodeRejection(t *testing.T) {
	ts, _ := spawnServer(t)
	resp := postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_x", "999999", "kakao_user_x", "https://callback.kakao.com/x"))
	body := decodeJSON(t, resp)
	if got := simpleText(body); got != "유효하지 않은 연결 코드입니다. KittyPaw 앱에서 새 코드를 확인하세요." {
		t.Fatalf("response = %q", got)
	}
}

func TestRateLimitExceeded(t *testing.T) {
	cfg := testConfig()
	cfg.DailyLimit = 2
	ts, _ := spawnServerWithConfig(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg := decodeJSON(t, postJSON(t, ts.URL+"/register", nil))
	token := reg["token"].(string)
	pairCode := reg["pair_code"].(string)
	conn, _, err := websocket.Dial(ctx, wsURL(ts, token), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("pair_act", pairCode, "user_rl", "https://callback.kakao.com/p")))
	r1 := decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_1", "hello", "user_rl", "https://callback.kakao.com/1")))
	if r1["useCallback"] != true {
		t.Fatalf("first webhook = %+v", r1)
	}
	var ignored map[string]any
	_ = wsjson.Read(ctx, conn, &ignored)

	r2 := decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_2", "hello again", "user_rl", "https://callback.kakao.com/2")))
	if got := simpleText(r2); got != "일일 사용 한도에 도달했습니다." {
		t.Fatalf("rate limited text = %q", got)
	}
}

func TestKillswitchBlocksWebhookWSStays(t *testing.T) {
	ts, _ := spawnServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg := decodeJSON(t, postJSON(t, ts.URL+"/register", nil))
	token := reg["token"].(string)
	conn, _, err := websocket.Dial(ctx, wsURL(ts, token), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	ksResp := postJSON(t, ts.URL+"/admin/killswitch?secret=test-secret", map[string]bool{"enabled": true})
	if ksResp.StatusCode != http.StatusOK {
		t.Fatalf("killswitch status = %d", ksResp.StatusCode)
	}
	_ = ksResp.Body.Close()

	resp := postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_ks", "hello", "user_ks", "https://callback.kakao.com/ks"))
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("webhook status = %d, want 503", resp.StatusCode)
	}
	_ = resp.Body.Close()

	if err := wsjson.Write(ctx, conn, map[string]string{"id": "test", "text": "ping"}); err != nil {
		t.Fatalf("websocket should stay writable: %v", err)
	}
}

func TestSSRFGuardRejectsNonKakao(t *testing.T) {
	ts, _ := spawnServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg := decodeJSON(t, postJSON(t, ts.URL+"/register", nil))
	token := reg["token"].(string)
	pairCode := reg["pair_code"].(string)
	conn, _, err := websocket.Dial(ctx, wsURL(ts, token), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("pair_ssrf", pairCode, "user_ssrf", "https://callback.kakao.com/p")))
	body := decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_ssrf", "hello", "user_ssrf", "https://evil.com/steal")))
	if got := simpleText(body); got != "일시적인 오류가 발생했습니다. 잠시 후 다시 시도해주세요." {
		t.Fatalf("ssrf response = %q", got)
	}
}

func TestUnpairedUserGetsGuide(t *testing.T) {
	ts, _ := spawnServer(t)
	body := decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_unp", "hello", "unknown_user", "https://callback.kakao.com/x")))
	if got := simpleText(body); got != "KittyPaw와 연결이 필요합니다. KittyPaw 앱에서 연결 코드를 확인하세요." {
		t.Fatalf("unpaired response = %q", got)
	}
}

func TestInvalidTokenWSReturns401(t *testing.T) {
	ts, _ := spawnServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL(ts, "nonexistent-token"), nil)
	if err == nil {
		t.Fatal("dial succeeded with invalid token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestOfflineSessionReturnsOfflineMessage(t *testing.T) {
	ts, _ := spawnServer(t)
	reg := decodeJSON(t, postJSON(t, ts.URL+"/register", nil))
	pairCode := reg["pair_code"].(string)
	decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("pair_off", pairCode, "user_offline", "https://callback.kakao.com/p")))

	body := decodeJSON(t, postJSON(t, ts.URL+"/webhook?secret=test-secret", webhookPayload("act_off", "hello", "user_offline", "https://callback.kakao.com/off")))
	if got := simpleText(body); got != "KittyPaw가 현재 오프라인 상태입니다. 앱을 실행 후 다시 시도해 주세요." {
		t.Fatalf("offline response = %q", got)
	}
}

func TestHealthEndpoint(t *testing.T) {
	ts, _ := spawnServer(t)
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	body := decodeJSON(t, resp)
	if body["status"] != "healthy" || body["version"] != "test-version" || body["commit"] != "test-commit" {
		t.Fatalf("health = %+v", body)
	}
}

func TestAdminStats(t *testing.T) {
	ts, _ := spawnServer(t)
	resp, err := http.Get(ts.URL + "/admin/stats?secret=test-secret")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	body := decodeJSON(t, resp)
	daily := body["daily"].(map[string]any)
	monthly := body["monthly"].(map[string]any)
	if daily["current"].(float64) != 0 || monthly["current"].(float64) != 0 {
		t.Fatalf("stats = %+v", body)
	}
	if body["killswitch"] != false {
		t.Fatalf("killswitch = %v", body["killswitch"])
	}
	if daily["limit"].(float64) == 0 || monthly["limit"].(float64) == 0 {
		t.Fatalf("limits missing: %+v", body)
	}
}

func TestWebhookRequiresAuth(t *testing.T) {
	ts, _ := spawnServer(t)
	resp := postJSON(t, ts.URL+"/webhook", webhookPayload("act", "hello", "user", "https://callback.kakao.com/x"))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no secret status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = postJSON(t, ts.URL+"/webhook?secret=wrong", webhookPayload("act", "hello", "user", "https://callback.kakao.com/x"))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func TestSSRFGuardAllowsKakao(t *testing.T) {
	if !IsAllowedCallbackHost("https://callback.kakao.com/abc") {
		t.Fatal("callback.kakao.com was blocked")
	}
	if !IsAllowedCallbackHost("https://api.kakaoenterprise.com/v1") {
		t.Fatal("api.kakaoenterprise.com was blocked")
	}
	if !IsAllowedCallbackHost("https://kakao.com/path") {
		t.Fatal("kakao.com was blocked")
	}
}

func TestSSRFGuardBlocksOthers(t *testing.T) {
	blocked := []string{
		"https://evil.com/steal",
		"https://kakao.com.evil.com/x",
		"https://fakekakao.com/path",
		"http://callback.kakao.com/abc",
		"file://kakao.com/etc/passwd",
		"not-a-url",
		"",
	}
	for _, raw := range blocked {
		if IsAllowedCallbackHost(raw) {
			t.Fatalf("%q was allowed", raw)
		}
	}
}
