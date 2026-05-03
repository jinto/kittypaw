package channel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jinto/kittypaw/core"
)

func TestTelegramPhotoUpdateEmitsImageAttachment(t *testing.T) {
	var getFileRequested bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			_, _ = io.WriteString(w, `{
				"ok": true,
				"result": [{
					"update_id": 1001,
					"message": {
						"message_id": 42,
						"from": {"id": 12345678, "first_name": "Jin"},
						"chat": {"id": 987654321},
						"caption": "이 사진 설명해줘",
						"photo": [
							{"file_id": "small-file", "file_unique_id": "small", "file_size": 100, "width": 90, "height": 90},
							{"file_id": "large-file", "file_unique_id": "large", "file_size": 1000, "width": 1024, "height": 768}
						]
					}
				}]
			}`)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			getFileRequested = true
			_, _ = io.WriteString(w, `{"ok":true,"result":{"file_path":"photos/cat.jpg"}}`)
		case strings.HasSuffix(r.URL.Path, "/sendChatAction"):
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	ch := NewTelegram("acct", "secret-token")
	ch.apiBase = srv.URL + "/bot"
	ch.client = srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events := make(chan core.Event, 1)
	errCh := make(chan error, 1)
	go func() { errCh <- ch.Start(ctx, events) }()

	select {
	case event := <-events:
		cancel()
		if event.Type != core.EventTelegram || event.AccountID != "acct" {
			t.Fatalf("event route = %s/%s", event.Type, event.AccountID)
		}
		payload, err := event.ParsePayload()
		if err != nil {
			t.Fatalf("parse payload: %v", err)
		}
		if payload.Text != "이 사진 설명해줘" {
			t.Fatalf("text = %q", payload.Text)
		}
		if len(payload.Attachments) != 1 {
			t.Fatalf("attachments = %#v", payload.Attachments)
		}
		att := payload.Attachments[0]
		if att.ID == "" || att.Type != "image" || att.Source != "telegram" {
			t.Fatalf("attachment metadata = %#v", att)
		}
		if !strings.Contains(att.URL, "/file/botsecret-token/photos/cat.jpg") {
			t.Fatalf("attachment URL = %q", att.URL)
		}
		if att.Caption != "이 사진 설명해줘" {
			t.Fatalf("caption = %q", att.Caption)
		}
	case err := <-errCh:
		t.Fatalf("telegram stopped before event: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for telegram photo event")
	}
	if !getFileRequested {
		t.Fatal("expected getFile request for photo attachment")
	}
}

func TestTelegramDocumentUpdateEmitsFileAttachment(t *testing.T) {
	var getFileRequested bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			_, _ = io.WriteString(w, `{
				"ok": true,
				"result": [{
					"update_id": 1002,
					"message": {
						"message_id": 43,
						"from": {"id": 12345678, "first_name": "Jin"},
						"chat": {"id": 987654321},
						"caption": "이 파일 봐줘",
						"document": {
							"file_id": "doc-file",
							"file_unique_id": "doc-unique",
							"file_name": "report.pdf",
							"mime_type": "application/pdf",
							"file_size": 2048
						}
					}
				}]
			}`)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			getFileRequested = true
			_, _ = io.WriteString(w, `{"ok":true,"result":{"file_path":"docs/report.pdf"}}`)
		case strings.HasSuffix(r.URL.Path, "/sendChatAction"):
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	ch := NewTelegram("acct", "secret-token")
	ch.apiBase = srv.URL + "/bot"
	ch.client = srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events := make(chan core.Event, 1)
	errCh := make(chan error, 1)
	go func() { errCh <- ch.Start(ctx, events) }()

	select {
	case event := <-events:
		cancel()
		payload, err := event.ParsePayload()
		if err != nil {
			t.Fatalf("parse payload: %v", err)
		}
		if len(payload.Attachments) != 1 {
			t.Fatalf("attachments = %#v", payload.Attachments)
		}
		att := payload.Attachments[0]
		if att.Type != "file" || att.Source != "telegram" {
			t.Fatalf("attachment metadata = %#v", att)
		}
		if att.FileName != "report.pdf" || att.MimeType != "application/pdf" || att.SizeBytes != 2048 {
			t.Fatalf("attachment file metadata = %#v", att)
		}
		if !strings.Contains(att.URL, "/file/botsecret-token/docs/report.pdf") {
			t.Fatalf("attachment URL = %q", att.URL)
		}
	case err := <-errCh:
		t.Fatalf("telegram stopped before event: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for telegram document event")
	}
	if !getFileRequested {
		t.Fatal("expected getFile request for document attachment")
	}
}

func TestKakaoRelayEventPreservesAttachments(t *testing.T) {
	msg := kakaoRelayMessage{
		ID:     "act-1",
		Text:   "이미지 봐줘",
		UserID: "kakao-user",
		Attachments: []core.ChatAttachment{{
			ID:      "kakao_media_0",
			Type:    "image",
			Source:  "kakao",
			URL:     "https://cdn.example.com/cat.png",
			Caption: "이미지 봐줘",
		}},
	}

	event, ok := kakaoRelayEvent("acct", msg)
	if !ok {
		t.Fatal("kakao relay event was dropped")
	}
	var payload core.ChatPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Attachments) != 1 {
		t.Fatalf("attachments = %#v", payload.Attachments)
	}
	if payload.Attachments[0].URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("attachment = %#v", payload.Attachments[0])
	}
}
