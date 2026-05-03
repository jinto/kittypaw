# Channel Image Delivery Implementation Plan

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render engine image responses as images in Telegram, KakaoTalk relay, and Web chat while preserving text fallback behavior.

**Architecture:** Add `core.OutboundResponse` with optional `core.ImageAttachment`, parse engine output once at delivery boundaries, and let channels opt into `channel.RichResponder`. Telegram sends photos directly, Kakao relay forwards public HTTPS image metadata, and Web chat renders image metadata in the browser.

**Tech Stack:** Go core/server/channel packages, Telegram Bot API, nhooyr WebSocket protocol, browser JavaScript/CSS, Rust axum relay with serde.

---

### Task 1: Core Outbound Response Model and Parser

**Files:**
- Create: `core/outbound.go`
- Test: `core/outbound_test.go`
- Modify: `core/protocol.go`

- [ ] **Step 1: Write the failing parser and protocol tests**

Create `core/outbound_test.go` with these tests:

```go
package core

import "testing"

func TestParseOutboundResponseMarkdownImage(t *testing.T) {
	out := ParseOutboundResponse("![space cat](https://cdn.example.com/cat.png)")
	if out.Text != "![space cat](https://cdn.example.com/cat.png)" {
		t.Fatalf("fallback text = %q", out.Text)
	}
	if out.Image == nil {
		t.Fatal("expected image attachment")
	}
	if out.Image.URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("url = %q", out.Image.URL)
	}
	if out.Image.Alt != "space cat" {
		t.Fatalf("alt = %q", out.Image.Alt)
	}
	if out.Image.Caption != "space cat" {
		t.Fatalf("caption = %q", out.Image.Caption)
	}
}

func TestParseOutboundResponseMarkdownImageWithCaption(t *testing.T) {
	out := ParseOutboundResponse("완성했습니다.\n\n![space cat](data:image/png;base64,aW1n)")
	if out.Image == nil {
		t.Fatal("expected image attachment")
	}
	if out.Image.Caption != "완성했습니다." {
		t.Fatalf("caption = %q", out.Image.Caption)
	}
}

func TestParseOutboundResponseSingleImageURL(t *testing.T) {
	out := ParseOutboundResponse("https://cdn.example.com/cat.webp?sig=1")
	if out.Image == nil {
		t.Fatal("expected image attachment")
	}
	if out.Image.URL != "https://cdn.example.com/cat.webp?sig=1" {
		t.Fatalf("url = %q", out.Image.URL)
	}
}

func TestParseOutboundResponseIgnoresNonImageMarkdownLink(t *testing.T) {
	out := ParseOutboundResponse("[docs](https://example.com/readme)")
	if out.Image != nil {
		t.Fatalf("unexpected image: %#v", out.Image)
	}
}

func TestNewDoneMsgForTurnWithOutboundIncludesImage(t *testing.T) {
	msg := NewDoneMsgForTurnWithOutbound("turn-1", OutboundResponse{
		Text: "fallback",
		Image: &ImageAttachment{
			URL:     "https://cdn.example.com/cat.png",
			Alt:     "cat",
			Caption: "caption",
		},
	}, nil)
	if msg.FullText != "fallback" || msg.TurnID != "turn-1" {
		t.Fatalf("message = %#v", msg)
	}
	if msg.Image == nil || msg.Image.URL != "https://cdn.example.com/cat.png" {
		t.Fatalf("image = %#v", msg.Image)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./core -run 'TestParseOutboundResponse|TestNewDoneMsgForTurnWithOutboundIncludesImage'`

Expected: compile failure for missing `ParseOutboundResponse`, `OutboundResponse`, `ImageAttachment`, and `NewDoneMsgForTurnWithOutbound`.

- [ ] **Step 3: Implement the core model and parser**

Create `core/outbound.go`:

```go
package core

import (
	"net/url"
	"regexp"
	"strings"
)

type ImageAttachment struct {
	URL     string `json:"url"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
}

type OutboundResponse struct {
	Text  string           `json:"text"`
	Image *ImageAttachment `json:"image,omitempty"`
}

var markdownImageRE = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)

func ParseOutboundResponse(text string) OutboundResponse {
	out := OutboundResponse{Text: text}
	match := markdownImageRE.FindStringSubmatchIndex(text)
	if match != nil {
		alt := text[match[2]:match[3]]
		imageURL := text[match[4]:match[5]]
		if isSupportedImageAttachmentURL(imageURL, true) {
			caption := strings.TrimSpace(text[:match[0]] + text[match[1]:])
			if caption == "" {
				caption = alt
			}
			out.Image = &ImageAttachment{URL: imageURL, Alt: alt, Caption: caption}
			return out
		}
	}

	trimmed := strings.TrimSpace(text)
	if isSupportedImageAttachmentURL(trimmed, false) {
		out.Image = &ImageAttachment{URL: trimmed}
	}
	return out
}

func isSupportedImageAttachmentURL(raw string, fromMarkdownImage bool) bool {
	if strings.HasPrefix(raw, "data:image/") && strings.Contains(raw, ";base64,") {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	if fromMarkdownImage {
		return true
	}
	path := strings.ToLower(parsed.Path)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
```

Modify `core/protocol.go`:

```go
type WsServerMsg struct {
	Type        string           `json:"type"`
	ID          string           `json:"id,omitempty"`
	FullText    string           `json:"full_text,omitempty"`
	TokensUsed  *int64           `json:"tokens_used,omitempty"`
	Message     string           `json:"message,omitempty"`
	Description string           `json:"description,omitempty"`
	Resource    string           `json:"resource,omitempty"`
	TurnID      string           `json:"turn_id,omitempty"`
	Image       *ImageAttachment `json:"image,omitempty"`
}

func NewDoneMsgFromOutbound(out OutboundResponse, tokensUsed *int64) WsServerMsg {
	return WsServerMsg{Type: WsMsgDone, FullText: out.Text, TokensUsed: tokensUsed, Image: out.Image}
}

func NewDoneMsgForTurnWithOutbound(turnID string, out OutboundResponse, tokensUsed *int64) WsServerMsg {
	return WsServerMsg{Type: WsMsgDone, FullText: out.Text, TokensUsed: tokensUsed, TurnID: turnID, Image: out.Image}
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./core -run 'TestParseOutboundResponse|TestNewDoneMsgForTurnWithOutboundIncludesImage'`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add core/outbound.go core/outbound_test.go core/protocol.go
git commit -m "feat: parse outbound image responses"
```

### Task 2: Rich Channel Dispatch Boundary

**Files:**
- Modify: `channel/channel.go`
- Modify: `server/server.go`
- Modify: `server/ws.go`
- Modify: `channel/websocket.go`
- Test: `server/channel_response_test.go`

- [ ] **Step 1: Write failing dispatch tests**

Create `server/channel_response_test.go`:

```go
package server

import (
	"context"
	"testing"

	"github.com/jinto/kittypaw/core"
)

type plainResponseChannel struct{ sent string }

func (p *plainResponseChannel) Start(context.Context, chan<- core.Event) error { return nil }
func (p *plainResponseChannel) Name() string                                   { return "plain" }
func (p *plainResponseChannel) SendResponse(_ context.Context, _, response, _ string) error {
	p.sent = response
	return nil
}

type richResponseChannel struct {
	plainResponseChannel
	rich core.OutboundResponse
}

func (r *richResponseChannel) SendRichResponse(_ context.Context, _ string, response core.OutboundResponse, _ string) error {
	r.rich = response
	return nil
}

func TestSendChannelResponseUsesRichResponder(t *testing.T) {
	ch := &richResponseChannel{}
	out := core.OutboundResponse{Text: "fallback", Image: &core.ImageAttachment{URL: "https://cdn.example.com/cat.png"}}
	if err := sendChannelResponse(context.Background(), ch, "chat", out, "reply"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if ch.rich.Image == nil || ch.rich.Image.URL != out.Image.URL {
		t.Fatalf("rich response = %#v", ch.rich)
	}
	if ch.sent != "" {
		t.Fatalf("plain response should not be used, got %q", ch.sent)
	}
}

func TestSendChannelResponseFallsBackToText(t *testing.T) {
	ch := &plainResponseChannel{}
	out := core.OutboundResponse{Text: "fallback", Image: &core.ImageAttachment{URL: "https://cdn.example.com/cat.png"}}
	if err := sendChannelResponse(context.Background(), ch, "chat", out, "reply"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if ch.sent != "fallback" {
		t.Fatalf("sent = %q", ch.sent)
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./server -run TestSendChannelResponse`

Expected: compile failure for missing `sendChannelResponse` and `channel.RichResponder`.

- [ ] **Step 3: Add rich interface and dispatch helper**

Modify `channel/channel.go`:

```go
type RichResponder interface {
	SendRichResponse(ctx context.Context, chatID string, response core.OutboundResponse, replyToMessageID string) error
}
```

Add helper to `server/server.go` near dispatch helpers:

```go
func sendChannelResponse(ctx context.Context, ch channel.Channel, chatID string, outbound core.OutboundResponse, replyToMessageID string) error {
	if rich, ok := ch.(channel.RichResponder); ok {
		return rich.SendRichResponse(ctx, chatID, outbound, replyToMessageID)
	}
	return ch.SendResponse(ctx, chatID, outbound.Text, replyToMessageID)
}
```

Update `server.dispatchLoop` after `session.Run`:

```go
outbound := core.ParseOutboundResponse(response)
if err := sendChannelResponse(ctx, ch, payload.ChatID, outbound, payload.ReplyToMessageID); err != nil {
	// keep existing logging and enqueue behavior, using outbound.Text
}
```

Update `server/ws.go`:

```go
outbound := core.ParseOutboundResponse(result)
sendWsMsg(ctx, conn, core.NewDoneMsgForTurnWithOutbound(clientMsg.TurnID, outbound, nil))
```

Update `channel/websocket.go`:

```go
func (w *WebSocketChannel) SendRichResponse(ctx context.Context, chatID string, response core.OutboundResponse, _ string) error {
	msg := core.NewDoneMsgFromOutbound(response, nil)
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return w.writeToClientOrBroadcast(ctx, chatID, data)
}
```

Extract the existing write loop from `SendResponse` into `writeToClientOrBroadcast(ctx, chatID string, data []byte) error` so both methods share routing.

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./server -run TestSendChannelResponse`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add channel/channel.go server/server.go server/ws.go channel/websocket.go server/channel_response_test.go
git commit -m "feat: route rich channel responses"
```

### Task 3: Telegram sendPhoto Support

**Files:**
- Modify: `channel/telegram.go`
- Test: `channel/telegram_image_test.go`

- [ ] **Step 1: Write failing Telegram rich response tests**

Create `channel/telegram_image_test.go`:

```go
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
```

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./channel -run TestTelegramSendRichResponse`

Expected: compile failure for missing `apiBase` and `SendRichResponse`.

- [ ] **Step 3: Implement Telegram rich response**

Modify `TelegramChannel`:

```go
apiBase string
```

Set it in `NewTelegram`:

```go
apiBase: telegramAPI,
```

Update `apiURL`:

```go
func (t *TelegramChannel) apiURL(method string) string {
	return t.apiBase + t.botToken + "/" + method
}
```

Add:

```go
func (t *TelegramChannel) SendRichResponse(ctx context.Context, chatIDStr string, response core.OutboundResponse, replyToMessageID string) error {
	if response.Image == nil || response.Image.URL == "" {
		return t.SendResponse(ctx, chatIDStr, response.Text, replyToMessageID)
	}
	chatID, replyToID, err := t.resolveSendTarget(chatIDStr, replyToMessageID)
	if err != nil {
		return err
	}
	t.stopTyping(chatID)
	if strings.HasPrefix(response.Image.URL, "data:image/") {
		return t.sendPhotoDataURI(ctx, chatID, response.Image.URL, telegramCaption(response), replyToID)
	}
	return t.sendPhotoURL(ctx, chatID, response.Image.URL, telegramCaption(response), replyToID)
}
```

Extract chat ID parsing from `SendResponse` into `resolveSendTarget(chatIDStr, replyToMessageID string) (chatID, replyToID int64, err error)`. Add `sendPhotoURL`, `sendPhotoDataURI`, `parseImageDataURI`, `telegramCaption`, and `checkTelegramResponse` helpers. Use JSON for URL photos and multipart for data URI photos.

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./channel -run TestTelegramSendRichResponse`

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add channel/telegram.go channel/telegram_image_test.go
git commit -m "feat: send telegram image responses"
```

### Task 4: Kakao Relay Image Frames

**Files:**
- Modify: `channel/kakao.go`
- Modify: `relay/src/types.rs`
- Modify: `relay/src/routes.rs`

- [ ] **Step 1: Write failing Kakao Go and Rust tests**

Add Go test to `channel/channel_test.go`:

```go
func TestKakaoRichResponseIncludesHTTPSImageFrame(t *testing.T) {
	msg := kakaoReplyMessage{
		ID:       "act-1",
		Text:     "fallback",
		ImageURL: "https://cdn.example.com/cat.png",
		ImageAlt: "cat",
	}
	data, _ := json.Marshal(msg)
	got := string(data)
	if !strings.Contains(got, `"image_url":"https://cdn.example.com/cat.png"`) {
		t.Fatalf("frame missing image_url: %s", got)
	}
	if !strings.Contains(got, `"image_alt":"cat"`) {
		t.Fatalf("frame missing image_alt: %s", got)
	}
}
```

Add Rust tests to `relay/src/types.rs` test module:

```rust
#[test]
fn ws_incoming_deserializes_image_fields() {
    let json = r#"{"id":"act_123","text":"response text","image_url":"https://cdn.example.com/cat.png","image_alt":"cat"}"#;
    let frame: WsIncoming = serde_json::from_str(json).unwrap();
    assert_eq!(frame.image_url.as_deref(), Some("https://cdn.example.com/cat.png"));
    assert_eq!(frame.image_alt.as_deref(), Some("cat"));
}

#[test]
fn kakao_image_serializes_simple_image() {
    let resp = kakao_image("https://cdn.example.com/cat.png", "cat");
    let json = serde_json::to_string(&resp).unwrap();
    assert!(json.contains("\"simpleImage\""));
    assert!(json.contains("\"imageUrl\":\"https://cdn.example.com/cat.png\""));
    assert!(json.contains("\"altText\":\"cat\""));
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./channel -run TestKakaoRichResponseIncludesHTTPSImageFrame
cargo test --manifest-path relay/Cargo.toml ws_incoming_deserializes_image_fields
```

Expected: compile failures for missing fields/functions.

- [ ] **Step 3: Implement Kakao Go rich frame**

Modify `kakaoReplyMessage`:

```go
type kakaoReplyMessage struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url,omitempty"`
	ImageAlt string `json:"image_alt,omitempty"`
}
```

Add `SendRichResponse`:

```go
func (k *KakaoChannel) SendRichResponse(ctx context.Context, actionID string, response core.OutboundResponse, _ string) error {
	imageURL := ""
	imageAlt := ""
	if response.Image != nil && isPublicHTTPSImageURL(response.Image.URL) {
		imageURL = response.Image.URL
		imageAlt = response.Image.Alt
	}
	return k.sendReply(ctx, kakaoReplyMessage{
		ID:       actionID,
		Text:     response.Text,
		ImageURL: imageURL,
		ImageAlt: imageAlt,
	})
}
```

Extract current `SendResponse` write logic into `sendReply(ctx, reply kakaoReplyMessage) error`. Add `isPublicHTTPSImageURL` in Go that accepts only `https` URLs with a non-empty host.

- [ ] **Step 4: Implement Rust relay simpleImage**

Modify `relay/src/types.rs`:

```rust
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KakaoOutput {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub simple_text: Option<KakaoSimpleText>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub simple_image: Option<KakaoSimpleImage>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct KakaoSimpleImage {
    pub image_url: String,
    pub alt_text: String,
}

pub fn kakao_image(image_url: &str, alt_text: &str) -> KakaoSimpleResponse {
    KakaoSimpleResponse {
        version: "2.0",
        template: KakaoTemplate {
            outputs: vec![KakaoOutput {
                simple_text: None,
                simple_image: Some(KakaoSimpleImage {
                    image_url: image_url.to_string(),
                    alt_text: alt_text.to_string(),
                }),
            }],
        },
    }
}

#[derive(Debug, Deserialize)]
pub struct WsIncoming {
    pub id: String,
    pub text: String,
    pub image_url: Option<String>,
    pub image_alt: Option<String>,
}
```

Update `kakao_text` to set `simple_text: Some(...)` and `simple_image: None`.

Modify `relay/src/routes.rs` so `handle_ws_message` passes the full `WsIncoming` to dispatch. In dispatch, choose:

```rust
let body = match incoming.image_url.as_deref() {
    Some(url) if is_public_https_image_url(url) => {
        kakao_image(url, incoming.image_alt.as_deref().unwrap_or("image"))
    }
    _ => kakao_text(&incoming.text),
};
```

- [ ] **Step 5: Run tests and verify they pass**

Run:

```bash
go test ./channel -run 'TestKakaoRichResponseIncludesHTTPSImageFrame|TestFromConfigKakao|TestKakaoSessionIDFromUserID'
cargo test --manifest-path relay/Cargo.toml ws_incoming_deserializes_image_fields
cargo test --manifest-path relay/Cargo.toml kakao_image_serializes_simple_image
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add channel/kakao.go channel/channel_test.go relay/src/types.rs relay/src/routes.rs
git commit -m "feat: send kakao image responses"
```

### Task 5: Web Chat Rendering

**Files:**
- Modify: `server/web/chat.js`
- Modify: `server/web/style.css`
- Test: existing Go protocol tests from Task 1 cover backend payload.

- [ ] **Step 1: Update Web chat rendering**

Modify the `done` case in `server/web/chat.js`:

```js
case 'done':
  if (this.currentBubble) {
    this._renderAssistantDone(this.currentBubble, msg);
    this.currentBubble.classList.remove('streaming');
    this.currentBubble = null;
    this._scrollToBottom();
  }
  this.busy = false;
  this.inputEl.disabled = false;
  this.inputEl.focus();
  break;
```

Add:

```js
_renderAssistantDone(el, msg) {
  const result = (msg.full_text || '').trim();
  const image = msg.image;
  if (!image || !image.url) {
    el.innerHTML = renderMarkdown(result);
    return;
  }

  const caption = (image.caption || '').trim();
  el.innerHTML = caption ? renderMarkdown(caption) : '';
  const img = document.createElement('img');
  img.className = 'chat-image';
  img.src = image.url;
  img.alt = image.alt || '';
  img.loading = 'lazy';
  el.appendChild(img);
}
```

Add CSS:

```css
.chat-image {
  display: block;
  max-width: min(100%, 420px);
  max-height: 420px;
  width: auto;
  height: auto;
  margin-top: 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--card-border);
}
```

- [ ] **Step 2: Run backend tests**

Run: `go test ./core ./server ./channel`

Expected: PASS.

- [ ] **Step 3: Commit**

Run:

```bash
git add server/web/chat.js server/web/style.css
git commit -m "feat: render web chat image responses"
```

### Task 6: Full Verification

**Files:**
- No code changes unless verification exposes a defect.

- [ ] **Step 1: Format Go and Rust code**

Run:

```bash
gofmt -w core/outbound.go core/outbound_test.go core/protocol.go channel/channel.go channel/telegram.go channel/telegram_image_test.go channel/kakao.go channel/channel_test.go channel/websocket.go server/server.go server/ws.go server/channel_response_test.go
cargo fmt --manifest-path relay/Cargo.toml
```

Expected: no output.

- [ ] **Step 2: Run full Go tests**

Run: `go test ./...`

Expected: PASS for all Go packages.

- [ ] **Step 3: Run relay tests**

Run: `cargo test --manifest-path relay/Cargo.toml`

Expected: PASS.

- [ ] **Step 4: Check git status**

Run: `git status --short --branch`

Expected: clean branch after all task commits.
