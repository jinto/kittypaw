# Channel Image Delivery Design

> Historical plan snapshot. This document records the implementation plan or design state at the time it was written; use repository README, ARCHITECTURE.md, and app README/DEPLOY docs for the current live shape.

## Goal

When the engine produces an image response, channels should render it as an image where the platform supports it instead of sending only markdown text such as `![alt](url)`.

The first implementation covers Telegram, KakaoTalk relay, and Web chat. It preserves existing text-only behavior for all other channels and for cases where image delivery is not possible.

## Scope

- Detect image responses in engine output before channel delivery.
- Support markdown image syntax: `![alt](url)`.
- Support a single image URL in otherwise plain output when it is clearly an image response.
- Keep the original text as fallback text.
- Telegram sends images with `sendPhoto`.
- KakaoTalk relay sends `simpleImage` only for public `https` image URLs.
- Web chat receives image attachment metadata and renders the image inline.

Out of scope:

- Hosting generated data URI images for KakaoTalk.
- Changing image generation providers.
- Adding image delivery to Slack or Discord.
- Sending multiple image galleries in one response.

## Architecture

Add a small outbound response model in Go, separate from `core.ChatPayload`:

- `Text`: fallback text to send when rich delivery is unavailable.
- `Image`: optional image attachment with `URL`, `Alt`, and `Caption`.

The server dispatch path parses the engine's response string into this model. Existing channels continue to receive `SendResponse(ctx, chatID, text, replyTo)` unless they implement an optional rich delivery interface.

Image-capable channels implement:

```go
type RichResponder interface {
    SendRichResponse(ctx context.Context, chatID string, response core.OutboundResponse, replyToMessageID string) error
}
```

Use `core.OutboundResponse`, `core.ImageAttachment`, and `channel.RichResponder` for the concrete names. The interface remains optional so existing channel implementations stay source-compatible.

## Data Flow

1. `Session.Run` returns a response string.
2. `server.dispatchLoop` converts the string into an outbound response.
3. If the selected channel implements the rich interface, the server calls it.
4. Otherwise the server falls back to existing `SendResponse`.

For retry queue behavior, pending responses remain text-only for this iteration. If rich send fails on Telegram or Web, enqueue the fallback text exactly as today. KakaoTalk action IDs are ephemeral and are not retried, matching existing behavior.

## Channel Behavior

### Telegram

- `https://...` image URL: call Bot API `sendPhoto` with `photo` set to the URL.
- `data:image/...;base64,...`: decode and upload as multipart `photo`.
- Caption uses the parsed caption or fallback text, bounded by Telegram caption limits.
- If `sendPhoto` fails, return the error so the server's existing fallback/retry behavior applies.

### KakaoTalk Relay

- Extend the Go relay frame from `{id,text}` to `{id,text,image_url?,image_alt?}`.
- Keep `text` required for fallback compatibility.
- Relay callback builds Kakao `simpleImage` only when `image_url` is public `https`.
- For data URI and unsupported URLs, relay sends `simpleText` with fallback text.

### Web Chat

- Extend the WebSocket done payload to include optional image attachment metadata.
- The browser renders an inline `<img>` for the attachment and keeps fallback text visible.
- Existing clients that ignore unknown fields continue to display text.

## Error Handling

- Parsing failures return the original text-only response.
- Unsupported image URLs fall back to text.
- KakaoTalk never attempts to render data URI images.
- Telegram data URI decode failures fall back through the normal send error path.
- The original text must never be dropped solely because image delivery fails.

## Tests

Go tests:

- Outbound parser extracts markdown image alt text and URL.
- Outbound parser handles single image URL.
- Parser ignores non-image markdown links.
- Telegram rich response uses `sendPhoto` for `https` image URLs.
- Telegram rich response uploads data URI images as multipart.
- Server dispatch uses rich interface when available and text fallback otherwise.
- WebSocket done message includes image attachment metadata.

Rust relay tests:

- WS incoming frame with `image_url` produces Kakao `simpleImage`.
- Data URI image URL falls back to `simpleText`.
- Legacy `{id,text}` frame still works.

Verification:

- `go test ./...`
- `cargo test` from `relay/` when dependency state allows; otherwise `cargo build` at minimum.
