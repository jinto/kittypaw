package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestTelegramSendRichResponseHTTPSPhoto(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer srv.Close()

	ch := NewTelegram("acct", "token")
	ch.apiBase = srv.URL + "/bot"

	out := core.OutboundResponse{
		Text:  "![cat](https://cdn.example.com/cat.png)",
		Image: &core.ImageAttachment{URL: "https://cdn.example.com/cat.png", Alt: "cat", Caption: "cat"},
	}
	if err := ch.SendRichResponse(context.Background(), "123", out, "456"); err != nil {
		t.Fatalf("send rich: %v", err)
	}
	if path != "/bottoken/sendPhoto" {
		t.Fatalf("path = %q", path)
	}
	if body["photo"] != "https://cdn.example.com/cat.png" {
		t.Fatalf("photo = %v", body["photo"])
	}
	if body["caption"] != "cat" {
		t.Fatalf("caption = %v", body["caption"])
	}
	if body["reply_to_message_id"].(float64) != 456 {
		t.Fatalf("reply_to_message_id = %v", body["reply_to_message_id"])
	}
}

func TestTelegramSendRichResponseDataURIPhoto(t *testing.T) {
	var path string
	var contentType string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer srv.Close()

	ch := NewTelegram("acct", "token")
	ch.apiBase = srv.URL + "/bot"

	out := core.OutboundResponse{
		Text:  "fallback",
		Image: &core.ImageAttachment{URL: "data:image/png;base64,aW1n", Caption: "caption"},
	}
	if err := ch.SendRichResponse(context.Background(), "123", out, ""); err != nil {
		t.Fatalf("send rich: %v", err)
	}
	if path != "/bottoken/sendPhoto" {
		t.Fatalf("path = %q", path)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data;") {
		t.Fatalf("content type = %q", contentType)
	}
	if !strings.Contains(body, `name="photo"; filename="image.png"`) {
		t.Fatalf("multipart missing photo file: %s", body)
	}
	if !strings.Contains(body, "img") {
		t.Fatalf("multipart missing decoded payload: %s", body)
	}
}
