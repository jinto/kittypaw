package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestExecuteHTTP_HeadersSupport(t *testing.T) {
	var gotHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		if r.Body != nil {
			_, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	s := &Session{Config: &core.Config{}}

	tests := []struct {
		name       string
		method     string
		args       []any
		wantHeader string
		wantValue  string
	}{
		{
			name:   "GET no headers",
			method: "get",
			args:   []any{ts.URL + "/test"},
		},
		{
			name:       "GET with Authorization header",
			method:     "get",
			args:       []any{ts.URL + "/test", map[string]any{"headers": map[string]any{"Authorization": "Bearer tok123"}}},
			wantHeader: "Authorization",
			wantValue:  "Bearer tok123",
		},
		{
			name:   "POST no headers",
			method: "post",
			args:   []any{ts.URL + "/test", `{"key":"val"}`},
		},
		{
			name:       "POST with custom header",
			method:     "post",
			args:       []any{ts.URL + "/test", `{"key":"val"}`, map[string]any{"headers": map[string]any{"X-Custom": "hello"}}},
			wantHeader: "X-Custom",
			wantValue:  "hello",
		},
		{
			name:   "GET with malformed options (string not object)",
			method: "get",
			args:   []any{ts.URL + "/test", "not-an-object"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHeaders = nil

			rawArgs := make([]json.RawMessage, len(tt.args))
			for i, a := range tt.args {
				b, err := json.Marshal(a)
				if err != nil {
					t.Fatal(err)
				}
				rawArgs[i] = b
			}

			call := core.SkillCall{
				SkillName: "Http",
				Method:    tt.method,
				Args:      rawArgs,
			}

			// httptest.NewServer binds to 127.0.0.1 which is private.
			// Use the bypass flag since we're testing headers, not SSRF.
			ctx := context.WithValue(context.Background(), httpValidatedHostKey, "127.0.0.1")
			result, err := executeHTTP(ctx, call, s)
			if err != nil {
				t.Fatalf("executeHTTP returned error: %v", err)
			}

			var resp map[string]any
			if err := json.Unmarshal([]byte(result), &resp); err != nil {
				t.Fatalf("invalid JSON response: %v", err)
			}
			if status, ok := resp["status"].(float64); !ok || status != 200 {
				t.Errorf("expected status 200, got %v", resp["status"])
			}

			if tt.wantHeader != "" {
				if gotHeaders == nil {
					t.Fatal("no headers received")
				}
				got := gotHeaders.Get(tt.wantHeader)
				if got != tt.wantValue {
					t.Errorf("header %q = %q, want %q", tt.wantHeader, got, tt.wantValue)
				}
			}
		})
	}
}

func TestExecuteHTTP_HostValidatedBypassesSSRF(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	s := &Session{Config: &core.Config{}}

	urlArg, _ := json.Marshal(ts.URL + "/test")
	call := core.SkillCall{
		SkillName: "Http",
		Method:    "get",
		Args:      []json.RawMessage{urlArg},
	}

	// Without bypass: httptest.NewServer is on 127.0.0.1 → blocked by SSRF.
	result, err := executeHTTP(context.Background(), call, s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["error"] == nil {
		t.Error("expected SSRF error for 127.0.0.1 without bypass, got success")
	}

	// With bypass flag: should succeed.
	ctx := context.WithValue(context.Background(), httpValidatedHostKey, "127.0.0.1")
	result, err = executeHTTP(ctx, call, s)
	if err != nil {
		t.Fatalf("unexpected error with bypass: %v", err)
	}
	var resp2 map[string]any
	if err := json.Unmarshal([]byte(result), &resp2); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp2["error"] != nil {
		t.Errorf("expected success with bypass flag, got error: %v", resp2["error"])
	}
	if status, ok := resp2["status"].(float64); !ok || status != 200 {
		t.Errorf("expected status 200, got %v", resp2["status"])
	}
}
