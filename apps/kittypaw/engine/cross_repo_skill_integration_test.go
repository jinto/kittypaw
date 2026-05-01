package engine

// IMPORTANT: This helper bypasses 4 of 5 production resolver layers
// (permissions, allowed_hosts, locale, ctx). Only the unwrap layer is
// simulated by mocks returning raw body strings directly. See Risk #5
// in .claude/plans/weather-now-kma-and-skill-test-harness.md.
//
// 3rd skill add → STOP, switch to PackageManager-based helper.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
	"github.com/jinto/kittypaw/sandbox"
)

const weatherNowSkillPath = "../../skills/packages/weather-now/main.js"

func TestOfficialPackagesDoNotParseRawUtterances(t *testing.T) {
	packagesDir := "../../skills/packages"
	entries, err := os.ReadDir(packagesDir)
	if errors.Is(err, fs.ErrNotExist) {
		if os.Getenv("CI") != "" {
			t.Fatalf("skills repo missing in CI: %s", packagesDir)
		}
		t.Skipf("skills repo not found locally: %s", packagesDir)
	}
	if err != nil {
		t.Fatalf("read packages dir: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(packagesDir, entry.Name(), "main.js")
		src, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		code := string(src)
		for _, forbidden := range []string{"ctx.message", "ctx.input", "message.text"} {
			if strings.Contains(code, forbidden) {
				t.Fatalf("%s must not parse raw utterance via %q", path, forbidden)
			}
		}
	}
}

// runExternalSkill loads main.js from a sibling skills repo, applies the
// production wrapping (mirrors engine/executor.go:1518-1532 — keep in sync),
// then executes via real goja sandbox with a SkillCall-level resolver mock.
//
// The sandbox call uses ExecutePackageOpts(... sandbox.Options{}) — a zero
// value mirrors the personal-account default selected at executor.go:1542.
//
// resolver receives raw core.SkillCall values; mock implementations should
// return post-unwrap raw body strings (the production unwrapHTTPBody layer
// is bypassed — Risk #5 in plan).
func runExternalSkill(
	t *testing.T,
	skillRelPath string,
	jsCtx map[string]any,
	resolver func(call core.SkillCall) (string, error),
) *core.ExecutionResult {
	t.Helper()

	// Cross-repo path may be missing locally; fail loudly in CI.
	src, err := os.ReadFile(skillRelPath)
	if errors.Is(err, fs.ErrNotExist) {
		if os.Getenv("CI") != "" {
			t.Fatalf("skills repo missing in CI: %s", skillRelPath)
		}
		t.Skipf("skills repo not found locally: %s", skillRelPath)
	}
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}

	// MIRROR: engine/executor.go:1526-1532 (wrapping only — keep in sync).
	// User-context injection at executor.go:1520-1525 is intentionally
	// bypassed here; jsCtx is provided directly by the caller.
	ctxJSON, err := json.Marshal(jsCtx)
	if err != nil {
		t.Fatalf("marshal jsCtx: %v", err)
	}
	ctxStr, err := json.Marshal(string(ctxJSON)) // double-marshal → JS string literal
	if err != nil {
		t.Fatalf("marshal ctxStr: %v", err)
	}
	syncCode := stripAwait(string(src))
	wrappedCode := fmt.Sprintf("const __context__ = %s;\n%s", string(ctxStr), syncCode)

	sb := sandbox.New(core.SandboxConfig{TimeoutSecs: 5})

	rawResolver := func(_ context.Context, call core.SkillCall) (string, error) {
		return resolver(call)
	}

	result, err := sb.ExecutePackageOpts(
		context.Background(),
		wrappedCode,
		map[string]any{},
		rawResolver,
		sandbox.Options{},
	)
	if err != nil {
		t.Fatalf("ExecutePackageOpts: %v", err)
	}
	return result
}

// httpRecorder is the canonical mock pattern for cross-repo skill tests.
// Tests build one and pass r.resolve as the runExternalSkill resolver.
type httpRecorder struct {
	kmaCalls  int
	wttrCalls int
	handler   func(u string) (string, error)
}

func (r *httpRecorder) resolve(call core.SkillCall) (string, error) {
	if call.SkillName != "Http" || call.Method != "get" {
		return "", fmt.Errorf("unmocked: %s.%s", call.SkillName, call.Method)
	}
	if len(call.Args) == 0 {
		return "", errors.New("Http.get: missing url arg")
	}
	var u string
	if err := json.Unmarshal(call.Args[0], &u); err != nil {
		return "", fmt.Errorf("decode url: %w", err)
	}
	switch mustHostname(u) {
	case "api.kittypaw.app":
		r.kmaCalls++
	case "wttr.in":
		r.wttrCalls++
	}
	return r.handler(u)
}

// mustHostname strict-parses the URL and rejects empty hostnames so a
// malformed URL doesn't slip past the recorder counters.
func mustHostname(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Hostname() == "" {
		panic("bad URL in skill: " + u) // surfaces as test failure
	}
	return parsed.Hostname()
}

// --- weather-now sub-tests --------------------------------------------------

// kmaNowcastEnvelope mirrors KMA's getUltraSrtNcst response — items have
// obsrValue (NOT fcstValue) and no SKY category (실황 only observes PTY).
const kmaNowcastEnvelope = `{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL"},"body":{"items":{"item":[
	{"baseDate":"20260430","baseTime":"1900","category":"T1H","obsrValue":"17.5"},
	{"baseDate":"20260430","baseTime":"1900","category":"REH","obsrValue":"55"},
	{"baseDate":"20260430","baseTime":"1900","category":"WSD","obsrValue":"2.5"},
	{"baseDate":"20260430","baseTime":"1900","category":"PTY","obsrValue":"0"},
	{"baseDate":"20260430","baseTime":"1900","category":"RN1","obsrValue":"0"}
]}}}}`

const kmaEmptyEnvelope = `{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL"},"body":{"items":{"item":[]}}}}`

const wttrHappyBody = `{"current_condition":[{"temp_C":"15","FeelsLikeC":"14","humidity":"60","weatherDesc":[{"value":"Sunny"}],"windspeedKmph":"10","winddir16Point":"N"}],"nearest_area":[{"areaName":[{"value":"San Francisco"}]}]}`

const kmaNowcastEnvelopeWithAttribution = `{"attribution":{"required":true,"label":"Weather data by KMA","url":"https://apihub.kma.go.kr"},"response":{"header":{"resultCode":"00","resultMsg":"NORMAL"},"body":{"items":{"item":[
	{"baseDate":"20260430","baseTime":"1900","category":"T1H","obsrValue":"17.5"},
	{"baseDate":"20260430","baseTime":"1900","category":"REH","obsrValue":"55"},
	{"baseDate":"20260430","baseTime":"1900","category":"WSD","obsrValue":"2.5"},
	{"baseDate":"20260430","baseTime":"1900","category":"PTY","obsrValue":"0"},
	{"baseDate":"20260430","baseTime":"1900","category":"RN1","obsrValue":"0"}
]}}}}`

const kmaSoonEnvelopeWithAttribution = `{"attribution":{"required":true,"label":"Weather data by KMA","url":"https://apihub.kma.go.kr"},"response":{"header":{"resultCode":"00","resultMsg":"NORMAL"},"body":{"items":{"item":[
	{"fcstDate":"20260430","fcstTime":"2000","category":"T1H","fcstValue":"15"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"SKY","fcstValue":"3"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"PTY","fcstValue":"0"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"REH","fcstValue":"55"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"WSD","fcstValue":"2.5"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"RN1","fcstValue":"강수없음"}
]}}}}`

func assertNoNoisyAttribution(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{"Powered by KittyPaw", "_Source:", "_Data:", "wttr.in", "Frankfurter"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output contains noisy attribution %q:\n%s", forbidden, output)
		}
	}
}

// TestWeatherNow_NaNGuard locks in the Number.isFinite() guard added in
// response to the prior review's NaN-bypass finding. If the guard regresses,
// the skill would build a `lat=NaN&lon=NaN` URL and call api.kittypaw.app —
// which would hit t.Fatalf in the mock and fail this test.
func TestWeatherNow_NaNGuard(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://wttr.in/") {
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL (NaN must skip KMA): %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "abc", // ParseFloat → NaN
			"longitude": "xyz",
			"location":  "Seoul",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q", result.Error)
	}
	if rec.kmaCalls != 0 {
		t.Errorf("NaN guard regressed: kmaCalls=%d (must be 0)", rec.kmaCalls)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected wttrCalls=1, got %d", rec.wttrCalls)
	}
}

// TestWeatherNow_KMAEmptyEnvelope locks in the cur=null fallthrough path
// (extractKMACurrent returns null when items is empty — NOT a throw). If
// future code raises an exception instead, the catch swallows it but the
// path is still through wttr fallback — this test is the only signal that
// cur=null specifically reached the if(cur) check.
func TestWeatherNow_KMAEmptyEnvelope(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/") {
			return kmaEmptyEnvelope, nil
		}
		if strings.HasPrefix(u, "https://wttr.in/") {
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success (fallback), got error=%q", result.Error)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected kmaCalls=1, got %d", rec.kmaCalls)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected wttrCalls=1 (cur=null fallthrough), got %d", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

func TestWeatherNow_NonKRPath(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://wttr.in/") {
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.77",
			"longitude": "-122.42",
			"location":  "San Francisco",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q", result.Error)
	}
	if rec.kmaCalls != 0 {
		t.Errorf("non-KR coords must skip KMA, got kmaCalls=%d", rec.kmaCalls)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected wttrCalls=1, got %d", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

func TestWeatherNow_KMAFailFallback(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/") {
			return "", errors.New("kma upstream 502")
		}
		if strings.HasPrefix(u, "https://wttr.in/") {
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success (fallback), got error=%q", result.Error)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected kmaCalls=1 (attempted then failed), got %d", rec.kmaCalls)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected wttrCalls=1 (fallback), got %d", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

func TestWeatherNow_KRHappyPath(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/v1/weather/kma/ultra-srt-ncst") {
			return kmaNowcastEnvelope, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected kmaCalls=1, got %d", rec.kmaCalls)
	}
	if rec.wttrCalls != 0 {
		t.Errorf("TDZ regression: KMA path threw and fell through to wttr (wttrCalls=%d)", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

func TestWeatherNow_KMAAttributionFromAPIPayload(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/v1/weather/kma/ultra-srt-ncst") {
			return kmaNowcastEnvelopeWithAttribution, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if !strings.Contains(result.Output, "Weather data by KMA") {
		t.Fatalf("required API attribution missing:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "Powered by KittyPaw") {
		t.Fatalf("KittyPaw brand footer must not be appended:\n%s", result.Output)
	}
}

func TestWeatherNow_UsesStructuredLocationParams(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/v1/weather/kma/ultra-srt-ncst") {
			parsed, err := url.Parse(u)
			if err != nil {
				t.Fatalf("parse url: %v", err)
			}
			if got := parsed.Query().Get("lat"); got != "37.4979" {
				t.Fatalf("lat = %q, want 37.4979", got)
			}
			if got := parsed.Query().Get("lon"); got != "127.0276" {
				t.Fatalf("lon = %q, want 127.0276", got)
			}
			return kmaNowcastEnvelope, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"location": "Seoul",
		},
		"params": map[string]any{
			"location": map[string]any{
				"label": "강남역",
				"lat":   37.4979,
				"lon":   127.0276,
			},
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected one KMA call, got kmaCalls=%d", rec.kmaCalls)
	}
	if rec.wttrCalls != 0 {
		t.Errorf("structured KR location should use KMA, got wttrCalls=%d", rec.wttrCalls)
	}
	if !strings.Contains(result.Output, "강남역 현재 날씨") {
		t.Errorf("output should label structured POI.\n--- got ---\n%s", result.Output)
	}
}

func TestWeatherNow_DoesNotParseRawMessageText(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://wttr.in/") {
			parsed, err := url.Parse(u)
			if err != nil {
				t.Fatalf("parse url: %v", err)
			}
			if got := strings.TrimPrefix(parsed.Path, "/"); got != "Seoul" {
				t.Fatalf("wttr location path = %q, want configured Seoul", got)
			}
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"location": "Seoul",
		},
		"message": map[string]string{
			"text": "강남역에 비오나? 지금?",
		},
	}
	result := runExternalSkill(t, weatherNowSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected configured wttr fallback call, got %d", rec.wttrCalls)
	}
	if rec.kmaCalls != 0 {
		t.Errorf("official JS must not geo-resolve raw message text, got kmaCalls=%d", rec.kmaCalls)
	}
}

// --- weather-soon sub-tests -------------------------------------------------

const weatherSoonSkillPath = "../../skills/packages/weather-soon/main.js"

// kmaForecastEnvelope mirrors KMA's getUltraSrtFcst response — items have
// fcstValue (NOT obsrValue), fcstDate/fcstTime per slot, SKY+PTY both present.
const kmaForecastEnvelope = `{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL"},"body":{"items":{"item":[
	{"fcstDate":"20260430","fcstTime":"2000","category":"T1H","fcstValue":"15"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"SKY","fcstValue":"3"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"PTY","fcstValue":"0"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"REH","fcstValue":"55"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"WSD","fcstValue":"2.5"},
	{"fcstDate":"20260430","fcstTime":"2000","category":"RN1","fcstValue":"강수없음"}
]}}}}`

func TestWeatherSoon_KRHappyPath(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/v1/weather/kma/ultra-srt-fcst") {
			return kmaForecastEnvelope, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherSoonSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected kmaCalls=1, got %d", rec.kmaCalls)
	}
	if rec.wttrCalls != 0 {
		t.Errorf("TDZ regression: KMA path threw and fell through to wttr (wttrCalls=%d)", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

func TestWeatherSoon_KMAAttributionFromAPIPayload(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/v1/weather/kma/ultra-srt-fcst") {
			return kmaSoonEnvelopeWithAttribution, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherSoonSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if !strings.Contains(result.Output, "Weather data by KMA") {
		t.Fatalf("required API attribution missing:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "Powered by KittyPaw") {
		t.Fatalf("KittyPaw brand footer must not be appended:\n%s", result.Output)
	}
}

func TestWeatherSoon_UsesStructuredLocationParams(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/v1/weather/kma/ultra-srt-fcst") {
			parsed, err := url.Parse(u)
			if err != nil {
				t.Fatalf("parse url: %v", err)
			}
			if got := parsed.Query().Get("lat"); got != "37.4979" {
				t.Fatalf("lat = %q, want 37.4979", got)
			}
			if got := parsed.Query().Get("lon"); got != "127.0276" {
				t.Fatalf("lon = %q, want 127.0276", got)
			}
			return kmaForecastEnvelope, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"location": "Seoul",
		},
		"params": map[string]any{
			"location": map[string]any{
				"label": "강남역",
				"lat":   37.4979,
				"lon":   127.0276,
			},
		},
	}
	result := runExternalSkill(t, weatherSoonSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected one KMA call, got kmaCalls=%d", rec.kmaCalls)
	}
	if rec.wttrCalls != 0 {
		t.Errorf("structured KR location should use KMA, got wttrCalls=%d", rec.wttrCalls)
	}
	if !strings.Contains(result.Output, "강남역") {
		t.Errorf("output should label structured POI.\n--- got ---\n%s", result.Output)
	}
}

func TestWeatherSoon_NonKRPath(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://wttr.in/") {
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.77",
			"longitude": "-122.42",
			"location":  "San Francisco",
		},
	}
	result := runExternalSkill(t, weatherSoonSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q", result.Error)
	}
	if rec.kmaCalls != 0 {
		t.Errorf("non-KR coords must skip KMA, got kmaCalls=%d", rec.kmaCalls)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected wttrCalls=1, got %d", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

func TestWeatherSoon_KMAFailFallback(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if strings.HasPrefix(u, "https://api.kittypaw.app/") {
			return "", errors.New("kma upstream 502")
		}
		if strings.HasPrefix(u, "https://wttr.in/") {
			return wttrHappyBody, nil
		}
		t.Fatalf("unexpected URL: %s", u)
		return "", nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"latitude":  "37.5665",
			"longitude": "126.978",
			"location":  "서울",
		},
	}
	result := runExternalSkill(t, weatherSoonSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success (fallback), got error=%q", result.Error)
	}
	if rec.kmaCalls != 1 {
		t.Errorf("expected kmaCalls=1 (attempted then failed), got %d", rec.kmaCalls)
	}
	if rec.wttrCalls != 1 {
		t.Errorf("expected wttrCalls=1 (fallback), got %d", rec.wttrCalls)
	}
	assertNoNoisyAttribution(t, result.Output)
}

const exchangeRateSkillPath = "../../skills/packages/exchange-rate/main.js"

func TestExchangeRate_UsesStructuredCurrencyParams(t *testing.T) {
	rec := &httpRecorder{handler: func(u string) (string, error) {
		if !strings.HasPrefix(u, "https://api.frankfurter.dev/v1/latest") {
			t.Fatalf("unexpected URL: %s", u)
		}
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatalf("parse url: %v", err)
		}
		if got := parsed.Query().Get("base"); got != "KRW" {
			t.Fatalf("base = %q, want KRW", got)
		}
		if got := parsed.Query().Get("symbols"); got != "USD,JPY" {
			t.Fatalf("symbols = %q, want USD,JPY", got)
		}
		return `{"date":"2026-05-01","base":"KRW","rates":{"USD":0.00068,"JPY":0.106}}`, nil
	}}

	jsCtx := map[string]any{
		"config": map[string]string{
			"base":    "USD",
			"symbols": "KRW",
		},
		"params": map[string]any{
			"base":    "KRW",
			"symbols": []any{"USD", "JPY"},
		},
	}
	result := runExternalSkill(t, exchangeRateSkillPath, jsCtx, rec.resolve)

	if !result.Success {
		t.Fatalf("expected success, got error=%q output=%q", result.Error, result.Output)
	}
	for _, want := range []string{"1 KRW = 0.00068 USD", "1 KRW = 0.106 JPY"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("output missing %q:\n%s", want, result.Output)
		}
	}
	assertNoNoisyAttribution(t, result.Output)
}
